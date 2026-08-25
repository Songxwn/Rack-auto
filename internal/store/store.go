package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Songxwn/Rack-auto/internal/model"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(8000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS machines (
  id TEXT PRIMARY KEY,
  name TEXT,
  mac TEXT UNIQUE,
  ip TEXT,
  status TEXT,
  firmware TEXT,
  boot_mode TEXT,
  bmc_type TEXT,
  bmc_address TEXT,
  bmc_port INTEGER,
  bmc_username TEXT,
  bmc_password TEXT,
  bmc_insecure INTEGER,
  tags TEXT,
  inventory TEXT,
  notes TEXT,
  agent_version TEXT,
  last_seen TEXT,
  created_at TEXT,
  updated_at TEXT
);
CREATE TABLE IF NOT EXISTS images (
  id TEXT PRIMARY KEY,
  name TEXT,
  os_family TEXT,
  kind TEXT,
  url TEXT,
  filename TEXT,
  checksum TEXT,
  checksum_type TEXT,
  size_b INTEGER,
  notes TEXT,
  created_at TEXT
);
CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  type TEXT,
  machine_id TEXT,
  image_id TEXT,
  status TEXT,
  params TEXT,
  progress INTEGER,
  message TEXT,
  logs TEXT,
  result TEXT,
  created_at TEXT,
  updated_at TEXT,
  started_at TEXT,
  finished_at TEXT
);
CREATE TABLE IF NOT EXISTS events (
  id TEXT PRIMARY KEY,
  level TEXT,
  message TEXT,
  machine_id TEXT,
  created_at TEXT
);
CREATE TABLE IF NOT EXISTS settings (
  k TEXT PRIMARY KEY,
  v TEXT
);
CREATE INDEX IF NOT EXISTS idx_jobs_machine ON jobs(machine_id, created_at);
CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at);
`)
	return err
}

func NewID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b)
}

func now() time.Time { return time.Now().UTC() }

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func parseTimePtr(s string) *time.Time {
	t := parseTime(s)
	if t.IsZero() {
		return nil
	}
	return &t
}

func (s *Store) UpsertMachine(m *model.Machine) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.ID == "" {
		m.ID = NewID("m")
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now()
	}
	m.UpdatedAt = now()
	tags, _ := json.Marshal(m.Tags)
	inv, _ := json.Marshal(m.Inventory)
	_, err := s.db.Exec(`INSERT INTO machines(
		id,name,mac,ip,status,firmware,boot_mode,bmc_type,bmc_address,bmc_port,bmc_username,bmc_password,bmc_insecure,tags,inventory,notes,agent_version,last_seen,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name, mac=excluded.mac, ip=excluded.ip, status=excluded.status, firmware=excluded.firmware,
		boot_mode=excluded.boot_mode, bmc_type=excluded.bmc_type, bmc_address=excluded.bmc_address, bmc_port=excluded.bmc_port,
		bmc_username=excluded.bmc_username, bmc_password=CASE WHEN excluded.bmc_password='' THEN machines.bmc_password ELSE excluded.bmc_password END,
		bmc_insecure=excluded.bmc_insecure, tags=excluded.tags, inventory=excluded.inventory, notes=excluded.notes,
		agent_version=excluded.agent_version, last_seen=excluded.last_seen, updated_at=excluded.updated_at`,
		m.ID, m.Name, strings.ToLower(m.MAC), m.IP, m.Status, m.Firmware, m.BootMode, m.BMCType, m.BMCAddress, m.BMCPort,
		m.BMCUsername, m.BMCPassword, boolInt(m.BMCInsecure), string(tags), string(inv), m.Notes, m.AgentVersion,
		fmtTimePtr(m.LastSeen), fmtTime(m.CreatedAt), fmtTime(m.UpdatedAt),
	)
	return err
}

func fmtTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return fmtTime(*t)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func scanMachine(sc interface{ Scan(dest ...any) error }) (model.Machine, error) {
	var m model.Machine
	var tags, inv, last, created, updated string
	var insecure int
	err := sc.Scan(&m.ID, &m.Name, &m.MAC, &m.IP, &m.Status, &m.Firmware, &m.BootMode, &m.BMCType, &m.BMCAddress, &m.BMCPort,
		&m.BMCUsername, &m.BMCPassword, &insecure, &tags, &inv, &m.Notes, &m.AgentVersion, &last, &created, &updated)
	if err != nil {
		return m, err
	}
	m.BMCInsecure = insecure != 0
	_ = json.Unmarshal([]byte(tags), &m.Tags)
	if strings.TrimSpace(inv) != "" && inv != "null" {
		var inventory model.Inventory
		if json.Unmarshal([]byte(inv), &inventory) == nil {
			m.Inventory = &inventory
		}
	}
	m.LastSeen = parseTimePtr(last)
	m.CreatedAt = parseTime(created)
	m.UpdatedAt = parseTime(updated)
	return m, nil
}

const machineCols = `id,name,mac,ip,status,firmware,boot_mode,bmc_type,bmc_address,bmc_port,bmc_username,bmc_password,bmc_insecure,tags,inventory,notes,agent_version,last_seen,created_at,updated_at`

func (s *Store) ListMachines() ([]model.Machine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT ` + machineCols + ` FROM machines ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Machine
	for rows.Next() {
		m, err := scanMachine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if out == nil {
		out = []model.Machine{}
	}
	return out, rows.Err()
}

func (s *Store) GetMachine(id string) (model.Machine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT `+machineCols+` FROM machines WHERE id=?`, id)
	m, err := scanMachine(row)
	if err == sql.ErrNoRows {
		return m, fmt.Errorf("machine not found")
	}
	return m, err
}

func (s *Store) GetMachineByMAC(mac string) (model.Machine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT `+machineCols+` FROM machines WHERE mac=?`, strings.ToLower(mac))
	m, err := scanMachine(row)
	if err == sql.ErrNoRows {
		return m, fmt.Errorf("machine not found")
	}
	return m, err
}

func (s *Store) DeleteMachine(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM machines WHERE id=?`, id)
	return err
}

func (s *Store) TouchMachine(id, ip, status, firmware, agentVer string, inv *model.Inventory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	invb, _ := json.Marshal(inv)
	_, err := s.db.Exec(`UPDATE machines SET ip=?, status=CASE WHEN ?!='' THEN ? ELSE status END, firmware=CASE WHEN ?!='' THEN ? ELSE firmware END,
		agent_version=?, inventory=?, last_seen=?, updated_at=? WHERE id=?`,
		ip, status, status, firmware, firmware, agentVer, string(invb), fmtTime(now()), fmtTime(now()), id)
	return err
}

func (s *Store) SetMachineStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE machines SET status=?, updated_at=? WHERE id=?`, status, fmtTime(now()), id)
	return err
}

func (s *Store) SetBootMode(id, mode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE machines SET boot_mode=?, updated_at=? WHERE id=?`, mode, fmtTime(now()), id)
	return err
}

func (s *Store) UpsertImage(img *model.Image) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if img.ID == "" {
		img.ID = NewID("img")
	}
	if img.CreatedAt.IsZero() {
		img.CreatedAt = now()
	}
	_, err := s.db.Exec(`INSERT INTO images(id,name,os_family,kind,url,filename,checksum,checksum_type,size_b,notes,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, os_family=excluded.os_family, kind=excluded.kind, url=excluded.url,
		filename=excluded.filename, checksum=excluded.checksum, checksum_type=excluded.checksum_type, size_b=excluded.size_b, notes=excluded.notes`,
		img.ID, img.Name, img.OSFamily, img.Kind, img.URL, img.Filename, img.Checksum, img.ChecksumType, img.SizeB, img.Notes, fmtTime(img.CreatedAt))
	return err
}

func scanImage(sc interface{ Scan(dest ...any) error }) (model.Image, error) {
	var img model.Image
	var created string
	err := sc.Scan(&img.ID, &img.Name, &img.OSFamily, &img.Kind, &img.URL, &img.Filename, &img.Checksum, &img.ChecksumType, &img.SizeB, &img.Notes, &created)
	img.CreatedAt = parseTime(created)
	return img, err
}

func (s *Store) ListImages() ([]model.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id,name,os_family,kind,url,filename,checksum,checksum_type,size_b,notes,created_at FROM images ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Image{}
	for rows.Next() {
		img, err := scanImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, rows.Err()
}

func (s *Store) GetImage(id string) (model.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT id,name,os_family,kind,url,filename,checksum,checksum_type,size_b,notes,created_at FROM images WHERE id=?`, id)
	img, err := scanImage(row)
	if err == sql.ErrNoRows {
		return img, fmt.Errorf("image not found")
	}
	return img, err
}

func (s *Store) DeleteImage(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM images WHERE id=?`, id)
	return err
}

func (s *Store) InsertJob(j *model.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j.ID == "" {
		j.ID = NewID("job")
	}
	if j.CreatedAt.IsZero() {
		j.CreatedAt = now()
	}
	j.UpdatedAt = now()
	params, _ := json.Marshal(j.Params)
	result, _ := json.Marshal(j.Result)
	_, err := s.db.Exec(`INSERT INTO jobs(id,type,machine_id,image_id,status,params,progress,message,logs,result,created_at,updated_at,started_at,finished_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.Type, j.MachineID, j.ImageID, j.Status, string(params), j.Progress, j.Message, j.Logs, string(result),
		fmtTime(j.CreatedAt), fmtTime(j.UpdatedAt), fmtTimePtr(j.StartedAt), fmtTimePtr(j.FinishedAt))
	return err
}

func scanJob(sc interface{ Scan(dest ...any) error }) (model.Job, error) {
	var j model.Job
	var params, result, created, updated, started, finished string
	err := sc.Scan(&j.ID, &j.Type, &j.MachineID, &j.ImageID, &j.Status, &params, &j.Progress, &j.Message, &j.Logs, &result, &created, &updated, &started, &finished)
	if err != nil {
		return j, err
	}
	if params != "" {
		_ = json.Unmarshal([]byte(params), &j.Params)
	}
	if result != "" && result != "null" {
		_ = json.Unmarshal([]byte(result), &j.Result)
	}
	j.CreatedAt = parseTime(created)
	j.UpdatedAt = parseTime(updated)
	j.StartedAt = parseTimePtr(started)
	j.FinishedAt = parseTimePtr(finished)
	return j, nil
}

func (s *Store) ListJobs(machineID string, limit int) ([]model.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id,type,machine_id,image_id,status,params,progress,message,logs,result,created_at,updated_at,started_at,finished_at FROM jobs`
	var args []any
	if machineID != "" {
		q += ` WHERE machine_id=?`
		args = append(args, machineID)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) GetJob(id string) (model.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT id,type,machine_id,image_id,status,params,progress,message,logs,result,created_at,updated_at,started_at,finished_at FROM jobs WHERE id=?`, id)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return j, fmt.Errorf("job not found")
	}
	return j, err
}

func (s *Store) NextPendingJob(machineID string) (model.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT id,type,machine_id,image_id,status,params,progress,message,logs,result,created_at,updated_at,started_at,finished_at
		FROM jobs WHERE machine_id=? AND status=? ORDER BY created_at ASC LIMIT 1`, machineID, model.JobPending)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return j, fmt.Errorf("no job")
	}
	return j, err
}

func (s *Store) UpdateJob(j model.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j.UpdatedAt = now()
	result, _ := json.Marshal(j.Result)
	params, _ := json.Marshal(j.Params)
	_, err := s.db.Exec(`UPDATE jobs SET status=?, params=?, progress=?, message=?, logs=?, result=?, updated_at=?, started_at=?, finished_at=? WHERE id=?`,
		j.Status, string(params), j.Progress, j.Message, j.Logs, string(result), fmtTime(j.UpdatedAt), fmtTimePtr(j.StartedAt), fmtTimePtr(j.FinishedAt), j.ID)
	return err
}

func (s *Store) AppendJobLog(id, line string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE jobs SET logs=COALESCE(logs,'')||?, updated_at=? WHERE id=?`, line+"\n", fmtTime(now()), id)
	return err
}

func (s *Store) AddEvent(level, message, machineID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec(`INSERT INTO events(id,level,message,machine_id,created_at) VALUES(?,?,?,?,?)`,
		NewID("ev"), level, message, machineID, fmtTime(now()))
	_, _ = s.db.Exec(`DELETE FROM events WHERE id NOT IN (SELECT id FROM events ORDER BY created_at DESC LIMIT 400)`)
}

func (s *Store) ListEvents(limit int) ([]model.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id,level,message,machine_id,created_at FROM events ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Event{}
	for rows.Next() {
		var e model.Event
		var created string
		if err := rows.Scan(&e.ID, &e.Level, &e.Message, &e.MachineID, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTime(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) Overview() (model.Overview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ov model.Overview
	ov.ByStatus = map[string]int{}
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM machines`).Scan(&ov.Machines)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM machines WHERE last_seen != '' AND last_seen > ?`, fmtTime(now().Add(-2*time.Minute))).Scan(&ov.Online)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM images`).Scan(&ov.Images)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&ov.Jobs)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE status=?`, model.JobRunning).Scan(&ov.Running)
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM machines GROUP BY status`)
	if err != nil {
		return ov, err
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return ov, err
		}
		ov.ByStatus[st] = n
	}
	return ov, nil
}

func (s *Store) Setting(key, fallback string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v string
	err := s.db.QueryRow(`SELECT v FROM settings WHERE k=?`, key).Scan(&v)
	if err != nil || v == "" {
		return fallback
	}
	return v
}

func (s *Store) SetSetting(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO settings(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, key, value)
	return err
}

func RedactJob(j model.Job) model.Job {
	switch p := j.Params.(type) {
	case map[string]any:
		cp := make(map[string]any, len(p))
		for k, v := range p {
			cp[k] = v
		}
		if _, ok := cp["password"]; ok {
			cp["password"] = ""
		}
		j.Params = cp
	}
	return j
}
