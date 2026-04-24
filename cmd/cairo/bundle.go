package main

// bundle.go implements cairo export / import / diff — the portable-identity
// surface. A .cairo bundle is a gzipped tar with a manifest.json and a copy
// of the cairo SQLite database. By default, the export excludes conversation
// artifacts (sessions and everything cascaded from them: messages, summaries,
// facts, jobs, tasks, task_artifacts) so the bundle carries identity without
// private history. --full re-enables history.

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/scotmcc/cairo/internal/db"
	_ "modernc.org/sqlite"
)

// --- manifest ---

// bundleManifest describes a .cairo bundle's contents. Versioned so future
// bundles can declare additive/incompatible shape changes; keep it simple for
// now — a single schema version for the DB contents.
type bundleManifest struct {
	Version         string         `json:"version"`
	ExportedAt      time.Time      `json:"exported_at"`
	IncludesHistory bool           `json:"includes_history"`
	Counts          map[string]int `json:"counts"`
}

const manifestVersion = "1"

// --- export ---

func runExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	full := fs.Bool("full", false, "include sessions, messages, summaries, facts, jobs, and tasks (default: omit conversation history)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: cairo export [--full] <output.cairo>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one output path")
	}
	out := fs.Arg(0)

	src := cairoDBPath()
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("source DB not found at %s — run cairo once to initialize before exporting", src)
	}

	// Copy the live DB into a temp file via VACUUM INTO. That's safer than
	// a plain file copy: sqlite handles WAL checkpointing for us and we get
	// a clean, consistent snapshot regardless of concurrent writers.
	tmp, err := os.CreateTemp("", "cairo-export-*.db")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	os.Remove(tmpPath) // VACUUM INTO refuses an existing target
	defer os.Remove(tmpPath)

	if err := vacuumInto(src, tmpPath); err != nil {
		return err
	}

	// If not --full, strip conversation history. Deleting from sessions
	// cascades through to messages/summaries/facts/jobs/tasks/task_artifacts
	// (the schema declares ON DELETE CASCADE on every one of those references).
	if !*full {
		if err := stripHistory(tmpPath); err != nil {
			return fmt.Errorf("strip history: %w", err)
		}
	}

	counts, err := countEntities(tmpPath)
	if err != nil {
		return fmt.Errorf("count entities: %w", err)
	}

	manifest := bundleManifest{
		Version:         manifestVersion,
		ExportedAt:      time.Now().UTC(),
		IncludesHistory: *full,
		Counts:          counts,
	}

	if err := writeBundle(out, tmpPath, manifest); err != nil {
		return err
	}

	fmt.Printf("exported to %s\n  format: %s\n  memories: %d  skills: %d  roles: %d  prompt_parts: %d\n",
		out,
		historyLabel(*full),
		counts["memories"], counts["skills"], counts["roles"], counts["prompt_parts"])
	return nil
}

// --- import ---

func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	force := fs.Bool("force", false, "skip the interactive confirmation")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: cairo import [--force] <bundle.cairo>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one bundle path")
	}
	in := fs.Arg(0)

	// Unpack to a temp dir so we can inspect the manifest before touching the
	// live DB.
	tmpDir, err := os.MkdirTemp("", "cairo-import-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	manifest, stagedDB, err := unpackBundle(in, tmpDir)
	if err != nil {
		return err
	}

	if manifest.Version != manifestVersion {
		return fmt.Errorf("bundle version %q does not match current cairo version %q", manifest.Version, manifestVersion)
	}

	fmt.Printf("bundle:\n  exported: %s\n  format:   %s\n  memories: %d  skills: %d  roles: %d  prompt_parts: %d\n",
		manifest.ExportedAt.Local().Format(time.RFC3339),
		historyLabel(manifest.IncludesHistory),
		manifest.Counts["memories"], manifest.Counts["skills"], manifest.Counts["roles"], manifest.Counts["prompt_parts"])

	dst := cairoDBPath()
	if _, err := os.Stat(dst); err == nil && !*force {
		fmt.Fprintln(os.Stderr, "this will REPLACE your current cairo identity with the contents of the bundle.")
		fmt.Fprint(os.Stderr, "a backup will be written alongside. proceed? [y/N]: ")
		var resp string
		fmt.Fscanln(os.Stdin, &resp)
		if resp != "y" && resp != "Y" && resp != "yes" {
			return fmt.Errorf("aborted")
		}
	}

	// Back up the current DB before replacing it. Restore is "mv the backup
	// back to cairo.db."
	if _, err := os.Stat(dst); err == nil {
		backup := fmt.Sprintf("%s.pre-import-%s", dst, time.Now().UTC().Format("20060102T150405Z"))
		if err := os.Rename(dst, backup); err != nil {
			return fmt.Errorf("backup current DB: %w", err)
		}
		fmt.Printf("backup: %s\n", backup)
	}

	if err := copyFile(stagedDB, dst); err != nil {
		return fmt.Errorf("install bundle DB: %w", err)
	}

	fmt.Printf("imported into %s — next cairo run uses the bundle's identity\n", dst)
	return nil
}

// --- diff ---

func runDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: cairo diff <bundle.cairo>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one bundle path")
	}
	in := fs.Arg(0)

	tmpDir, err := os.MkdirTemp("", "cairo-diff-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	manifest, stagedDB, err := unpackBundle(in, tmpDir)
	if err != nil {
		return err
	}

	local := cairoDBPath()
	if _, err := os.Stat(local); err != nil {
		return fmt.Errorf("no local cairo DB at %s to diff against", local)
	}
	localCounts, err := countEntities(local)
	if err != nil {
		return fmt.Errorf("count local: %w", err)
	}

	fmt.Printf("bundle (%s) vs local:\n", manifest.ExportedAt.Local().Format(time.RFC3339))
	keys := []string{"memories", "skills", "notes", "roles", "prompt_parts", "custom_tools", "config_keys"}
	for _, k := range keys {
		l := localCounts[k]
		b := manifest.Counts[k]
		marker := " "
		if l != b {
			marker = "*"
		}
		fmt.Printf("  %s %-15s local=%-4d bundle=%-4d\n", marker, k, l, b)
	}

	// Soul comparison — same tool's persona spec.
	localSoul, _ := readConfigValue(local, "soul_prompt")
	bundleSoul, _ := readConfigValue(stagedDB, "soul_prompt")
	if localSoul != bundleSoul {
		fmt.Println("\nsoul differs:")
		fmt.Printf("  local:  %s\n", trunc(localSoul, 200))
		fmt.Printf("  bundle: %s\n", trunc(bundleSoul, 200))
	} else {
		fmt.Println("\nsoul matches")
	}

	// Role model assignments — who runs what model.
	if err := printRoleModelDiff(local, stagedDB); err != nil {
		fmt.Fprintf(os.Stderr, "role model diff: %v\n", err)
	}

	return nil
}

// --- helpers ---

func cairoDBPath() string {
	return filepath.Join(db.DefaultDataDir(), "cairo.db")
}

func historyLabel(full bool) string {
	if full {
		return "full (includes sessions, messages, summaries, facts, jobs, tasks)"
	}
	return "identity-only (no conversation history)"
}

func vacuumInto(src, dst string) error {
	sqldb, err := sql.Open("sqlite", src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer sqldb.Close()
	// VACUUM INTO refuses an existing target and rejects a path with special
	// chars via the normal parser. Quote defensively.
	quoted := "'" + dst + "'"
	if _, err := sqldb.Exec("VACUUM INTO " + quoted); err != nil {
		return fmt.Errorf("vacuum into %s: %w", dst, err)
	}
	return nil
}

func stripHistory(path string) error {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer sqldb.Close()
	// PRAGMA foreign_keys is per-connection and off by default. Pin the pool
	// to one connection so the PRAGMA is guaranteed to apply to the DELETE
	// that follows; the DSN _foreign_keys=on form turned out to be unreliable
	// here (PRAGMA read back as 0 on the next connection).
	sqldb.SetMaxOpenConns(1)
	if _, err := sqldb.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable foreign_keys: %w", err)
	}
	if _, err := sqldb.Exec("DELETE FROM sessions"); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	// Reclaim space after the cascaded deletes.
	if _, err := sqldb.Exec("VACUUM"); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	return nil
}

func countEntities(path string) (map[string]int, error) {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer sqldb.Close()
	out := make(map[string]int)
	tables := []string{"memories", "skills", "notes", "roles", "prompt_parts", "custom_tools", "sessions", "messages", "summaries", "facts", "jobs", "tasks"}
	for _, t := range tables {
		var n int
		if err := sqldb.QueryRow("SELECT COUNT(*) FROM " + t).Scan(&n); err != nil {
			// Table may not exist in very old DBs — skip, don't fail.
			continue
		}
		out[t] = n
	}
	// config keys count is useful in diff but covers a scalar table, count entries.
	var n int
	if err := sqldb.QueryRow("SELECT COUNT(*) FROM config").Scan(&n); err == nil {
		out["config_keys"] = n
	}
	return out, nil
}

func readConfigValue(path, key string) (string, error) {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return "", err
	}
	defer sqldb.Close()
	var val string
	err = sqldb.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// printRoleModelDiff compares role→model assignments between two DBs.
// Minimal and targeted — the thing a user most cares about is "which role
// runs which model" since that's the practical effect of swapping identities.
func printRoleModelDiff(localPath, bundlePath string) error {
	localMap, err := roleModelMap(localPath)
	if err != nil {
		return err
	}
	bundleMap, err := roleModelMap(bundlePath)
	if err != nil {
		return err
	}
	names := make(map[string]struct{})
	for k := range localMap {
		names[k] = struct{}{}
	}
	for k := range bundleMap {
		names[k] = struct{}{}
	}
	var diffs []string
	for name := range names {
		if localMap[name] != bundleMap[name] {
			diffs = append(diffs, fmt.Sprintf("  %s: local=%q bundle=%q", name, localMap[name], bundleMap[name]))
		}
	}
	if len(diffs) == 0 {
		fmt.Println("role→model assignments match")
		return nil
	}
	fmt.Println("\nrole→model differs:")
	for _, d := range diffs {
		fmt.Println(d)
	}
	return nil
}

func roleModelMap(path string) (map[string]string, error) {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer sqldb.Close()
	rows, err := sqldb.Query("SELECT name, model FROM roles")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var n, m string
		if err := rows.Scan(&n, &m); err != nil {
			return nil, err
		}
		out[n] = m
	}
	return out, rows.Err()
}

func writeBundle(outPath, dbPath string, manifest bundleManifest) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	// manifest.json
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: "manifest.json",
		Mode: 0644,
		Size: int64(len(manifestBytes)),
	}); err != nil {
		return err
	}
	if _, err := tw.Write(manifestBytes); err != nil {
		return err
	}

	// cairo.db
	dbInfo, err := os.Stat(dbPath)
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: "cairo.db",
		Mode: 0644,
		Size: dbInfo.Size(),
	}); err != nil {
		return err
	}
	dbFile, err := os.Open(dbPath)
	if err != nil {
		return err
	}
	defer dbFile.Close()
	if _, err := io.Copy(tw, dbFile); err != nil {
		return err
	}
	return nil
}

func unpackBundle(bundlePath, dir string) (bundleManifest, string, error) {
	var manifest bundleManifest
	f, err := os.Open(bundlePath)
	if err != nil {
		return manifest, "", fmt.Errorf("open bundle: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return manifest, "", fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var stagedDB string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return manifest, "", fmt.Errorf("tar: %w", err)
		}
		// Defense against tar traversal — only allow bare names.
		if filepath.Base(hdr.Name) != hdr.Name {
			return manifest, "", fmt.Errorf("bundle contains suspicious path %q", hdr.Name)
		}
		out := filepath.Join(dir, hdr.Name)
		switch hdr.Name {
		case "manifest.json":
			data, err := io.ReadAll(tr)
			if err != nil {
				return manifest, "", err
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return manifest, "", fmt.Errorf("parse manifest: %w", err)
			}
			if err := os.WriteFile(out, data, 0644); err != nil {
				return manifest, "", err
			}
		case "cairo.db":
			w, err := os.Create(out)
			if err != nil {
				return manifest, "", err
			}
			if _, err := io.Copy(w, tr); err != nil {
				w.Close()
				return manifest, "", err
			}
			w.Close()
			stagedDB = out
		default:
			// Ignore unknown entries for forward compatibility.
		}
	}
	if manifest.Version == "" {
		return manifest, "", fmt.Errorf("bundle is missing manifest.json")
	}
	if stagedDB == "" {
		return manifest, "", fmt.Errorf("bundle is missing cairo.db")
	}
	return manifest, stagedDB, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
