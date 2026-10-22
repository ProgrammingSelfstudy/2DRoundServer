package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	backupPrefix = "2droundserver-"
	backupSuffix = ".db"
)

// BackupFile is one backup in a directory.
type BackupFile struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
}

// Backup writes a consistent copy of the live database into dir and returns its path.
// It uses SQLite's VACUUM INTO, so the server keeps running and no half-written page
// can be copied. The copy is opened and checked before it is returned; a copy that
// fails the check is removed and reported as an error.
func (s *Store) Backup(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := s.now().Format("20060102-150405")
	path := filepath.Join(dir, backupPrefix+stamp+backupSuffix)
	for i := 2; ; i++ { // two backups in the same second get distinct names
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("%s%s-%d%s", backupPrefix, stamp, i, backupSuffix))
	}
	if _, err := s.db.Exec(`VACUUM INTO ?`, path); err != nil {
		os.Remove(path)
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		os.Remove(path)
		return "", err
	}
	if err := verifyDatabase(path); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("backup failed its check: %w", err)
	}
	return path, nil
}

// verifyDatabase opens a database file read-only and runs SQLite's integrity check.
func verifyDatabase(path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var res string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("integrity_check: %s", res)
	}
	return nil
}

// ListBackups returns the backups in dir, newest first (the names carry the time).
func ListBackups(dir string) ([]BackupFile, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []BackupFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []BackupFile{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, backupPrefix) || !strings.HasSuffix(n, backupSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupFile{Name: n, Size: info.Size(), ModTime: info.ModTime().Unix()})
	}
	sort.Slice(out, func(i, j int) bool { return backupNewer(out[i].Name, out[j].Name) })
	return out, nil
}

// backupKey splits a backup's name into its timestamp and its sequence number: backups
// made in the same second are "<stamp>.db", "<stamp>-2.db", "<stamp>-3.db", ... A plain
// string comparison would put "<stamp>.db" after "<stamp>-3.db" ('.' sorts above '-').
func backupKey(name string) (stamp string, seq int) {
	core := strings.TrimSuffix(strings.TrimPrefix(name, backupPrefix), backupSuffix)
	seq = 1
	if i := strings.LastIndex(core, "-"); i > 0 && i != strings.Index(core, "-") { // a second "-": stamp is date-time
		if n, err := strconv.Atoi(core[i+1:]); err == nil {
			return core[:i], n
		}
	}
	return core, seq
}

// backupNewer reports whether backup a was made after backup b.
func backupNewer(a, b string) bool {
	sa, na := backupKey(a)
	sb, nb := backupKey(b)
	if sa != sb {
		return sa > sb
	}
	return na > nb
}

// PruneBackups deletes all but the newest keep backups in dir and returns how many it
// deleted. Files that are not backups are never touched. keep < 1 deletes nothing.
func PruneBackups(dir string, keep int) (int, error) {
	if keep < 1 {
		return 0, nil
	}
	all, err := ListBackups(dir)
	if err != nil || len(all) <= keep {
		return 0, err
	}
	n := 0
	for _, b := range all[keep:] {
		if err := os.Remove(filepath.Join(dir, b.Name)); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// BackupNow makes a backup and then prunes the old ones.
func (s *Store) BackupNow(dir string, keep int) (path string, pruned int, err error) {
	if path, err = s.Backup(dir); err != nil {
		return "", 0, err
	}
	pruned, err = PruneBackups(dir, keep)
	return path, pruned, err
}

// BackupDue reports whether the newest backup in dir is older than every (or there is none).
func BackupDue(dir string, every time.Duration, now time.Time) bool {
	all, err := ListBackups(dir)
	if err != nil || len(all) == 0 {
		return true
	}
	return now.Sub(time.Unix(all[0].ModTime, 0)) >= every
}
