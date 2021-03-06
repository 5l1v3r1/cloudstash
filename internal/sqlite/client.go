package sqlite

import (
	"database/sql"
	"fmt"

	"github.com/paddlesteamer/cloudstash/internal/common"

	// sqlite3 driver
	_ "github.com/mattn/go-sqlite3"
)

// Client is created when a new connection to the database is established
// Use NewClient() to create
type Client struct {
	db *sql.DB
}

var (
	tableSchemas = [...]string{
		`CREATE TABLE files (
		"inode"  INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"name"   TEXT NOT NULL,
		"url"    TEXT NOT NULL DEFAULT "",
		"size"   INTEGER NOT NULL DEFAULT 0,
		"mode"   INTEGER NOT NULL,
		"parent" INTEGER NOT NULL,
		"type"   INTEGER NOT NULL,
		"hash"   TEXT NOT NULL DEFAULT "",
		UNIQUE("name", "parent"),
		FOREIGN KEY("parent") REFERENCES folders("id")
	);`,
		fmt.Sprintf(`INSERT INTO files(inode, name, mode, parent, type) VALUES (1, "", 493, 0, %d);`, common.DrvFolder), // root folder with mode 0755
	}
)

// InitDB initializes tables. Supposed to be called on the very first run.
func InitDB(path string) error {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return fmt.Errorf("couldn't open db at %s: %v", path, err)
	}
	defer db.Close()

	for _, sqlStr := range tableSchemas {
		st, err := db.Prepare(sqlStr)
		if err != nil {
			return fmt.Errorf("error in query `%s`: %v", sqlStr, err)
		}

		_, err = st.Exec()
		if err != nil {
			return fmt.Errorf("couldn't execute initialization query: %v", err)
		}
	}

	return nil
}

// NewClient returns a new database connection.
func NewClient(filePath string) (*Client, error) {
	db, err := sql.Open("sqlite3", filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open DB at %s: %v", filePath, err)
	}

	return &Client{db}, nil
}

// Close terminates database connection.
func (c *Client) Close() {
	c.db.Close()
}

// IsValidDatabase checks whether `files` table exists
func (c *Client) IsValidDatabase() bool {
	query, _ := c.db.Prepare("SELECT * FROM files LIMIT 1")

	row, err := query.Query()
	if err != nil {
		return false
	}
	defer row.Close()

	return true
}

// Search looks for file with specified name under specified inode
func (c *Client) Search(parent int64, name string) (*Metadata, error) {
	query, err := c.db.Prepare("SELECT * FROM files WHERE name=? and parent=?")
	if err != nil {
		return nil, fmt.Errorf("couldn't prepare statement: %v", err)
	}

	row, err := query.Query(name, parent)
	if err != nil {
		return nil, fmt.Errorf("there is an error in query: %v", err)
	}
	defer row.Close()

	if !row.Next() {
		return nil, common.ErrNotFound
	}

	return c.parseRow(row)
}

// Get returns file metadata with specified inode
func (c *Client) Get(inode int64) (*Metadata, error) {
	query, err := c.db.Prepare("SELECT * FROM files WHERE inode=?")
	if err != nil {
		return nil, fmt.Errorf("couldn't prepare statement: %v", err)
	}

	row, err := query.Query(inode)
	if err != nil {
		return nil, fmt.Errorf("there is an error in query: %v", err)
	}
	defer row.Close()

	if !row.Next() {
		return nil, common.ErrNotFound
	}

	md, err := c.parseRow(row)
	if err != nil {
		return nil, err
	}

	err = c.fillNLink(md)
	if err != nil {
		return nil, fmt.Errorf("couldn't get nlink count: %v", err)
	}

	return md, nil
}

// Delete removes file with specified inode from database
func (c *Client) Delete(inode int64) error {
	query, err := c.db.Prepare("DELETE FROM files WHERE inode=?")
	if err != nil {
		return fmt.Errorf("couldn't prepare statement: %v", err)
	}

	_, err = query.Exec(inode)
	if err != nil {
		return fmt.Errorf("couldn't delete entry: %v", err)
	}

	return nil
}

// GetChildren returns files under the folder with specified inode
func (c *Client) GetChildren(parent int64) ([]Metadata, error) {
	query, err := c.db.Prepare("SELECT * FROM files WHERE parent=?")
	if err != nil {
		return nil, fmt.Errorf("couldn't prepare statement: %v", err)
	}

	row, err := query.Query(parent)
	if err != nil {
		return nil, fmt.Errorf("there is an error in query: %v", err)
	}
	defer row.Close()

	mdList := []Metadata{}
	for row.Next() {
		md, err := c.parseRow(row)
		if err != nil {
			return nil, err
		}

		err = c.fillNLink(md)
		if err != nil {
			return nil, fmt.Errorf("couldn't get nlink count: %v", err)
		}

		mdList = append(mdList, *md)
	}

	return mdList, nil
}

// AddDirectory insert row with type folder into the database
func (c *Client) AddDirectory(parent int64, name string, mode int) (*Metadata, error) {
	query, err := c.db.Prepare("INSERT INTO files(name, mode, parent, type) VALUES(?, ?, ?, ?)")
	if err != nil {
		return nil, fmt.Errorf("couldn't prepare statement: %v", err)
	}

	_, err = query.Exec(name, mode, parent, common.DrvFolder)
	if err != nil {
		return nil, fmt.Errorf("couldn't insert directory: %v", err)
	}

	query, err = c.db.Prepare("SELECT * FROM files WHERE name=? and parent=?")
	if err != nil {
		return nil, fmt.Errorf("couldn't prepare statement: %v", err)
	}

	row, err := query.Query(name, parent)
	if err != nil {
		return nil, fmt.Errorf("there is an error in query: %v", err)
	}
	defer row.Close()

	if !row.Next() {
		return nil, fmt.Errorf("row should be inserted but apparently it didn't")
	}

	md, err := c.parseRow(row)
	if err != nil {
		return nil, err
	}

	// since the directory has just been created, there are only '.' and '..'
	md.NLink = 2
	return md, nil
}

// CreateFile insert row with type file into the database
func (c *Client) CreateFile(parent int64, name string, mode int, url string, hash string) (*Metadata, error) {
	query, err := c.db.Prepare("INSERT INTO files(name, url, size, mode, parent, type, hash) VALUES(?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return nil, fmt.Errorf("couldn't prepare statement: %v", err)
	}

	_, err = query.Exec(name, url, 0, mode, parent, common.DrvFile, hash)
	if err != nil {
		return nil, fmt.Errorf("couldn't insert file: %v", err)
	}

	query, err = c.db.Prepare("SELECT * FROM files WHERE name=? and parent=?")
	if err != nil {
		return nil, fmt.Errorf("couldn't prepare statement: %v", err)
	}

	row, err := query.Query(name, parent)
	if err != nil {
		return nil, fmt.Errorf("there is an error in query: %v", err)
	}
	defer row.Close()

	if !row.Next() {
		return nil, fmt.Errorf("row should be inserted but apparently it didn't")
	}

	md, err := c.parseRow(row)
	if err != nil {
		return nil, err
	}

	// it's file and hardlink isn't supported
	md.NLink = 1
	return md, nil
}

// Update updates related row with new metadata
func (c *Client) Update(md *Metadata) error {
	query, err := c.db.Prepare("UPDATE files SET name=?, url=?, size=?, mode=?, parent=?, type=?, hash=? WHERE inode=?")
	if err != nil {
		return fmt.Errorf("couldn't prepare statement: %v", err)
	}

	_, err = query.Exec(md.Name, md.URL, md.Size, md.Mode, md.Parent, md.Type, md.Hash, md.Inode)
	if err != nil {
		return fmt.Errorf("couldn't update file: %v", err)
	}

	return nil
}

// GetRows returns rows starting from specified offset with specified limit
func (c *Client) GetRows(limit int, offset int) ([]Metadata, error) {
	query, err := c.db.Prepare("SELECT * FROM files LIMIT ? OFFSET ?")
	if err != nil {
		return nil, fmt.Errorf("couldn't prepare statement: %v", err)
	}

	row, err := query.Query()
	if err != nil {
		return nil, fmt.Errorf("there is an error in query: %v", err)
	}
	defer row.Close()

	mdList := []Metadata{}
	for row.Next() {
		md, err := c.parseRow(row)
		if err != nil {
			return nil, err
		}

		err = c.fillNLink(md)
		if err != nil {
			return nil, fmt.Errorf("couldn't get nlink count: %v", err)
		}

		mdList = append(mdList, *md)
	}

	return mdList, nil
}

// GetRowCount returns total number of rows
func (c *Client) GetRowCount() (int, error) {
	query, err := c.db.Prepare("SELECT count(*) FROM files")
	if err != nil {
		return 0, fmt.Errorf("couldn't prepare statement: %v", err)
	}

	row, err := query.Query()
	if err != nil {
		return 0, fmt.Errorf("there is an error in query: %v", err)
	}
	defer row.Close()

	var count int

	row.Next() // no need to check return value

	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("couldn't get row count: %v", err)
	}

	return count, nil
}

// GetFileCount returns number of rows of type common.DrvFile
func (c *Client) GetFileCount() (int64, error) {
	query, err := c.db.Prepare("SELECT count(*) FROM files where type=?")
	if err != nil {
		return 0, fmt.Errorf("couldn't prepare statement: %v", err)
	}

	row, err := query.Query(common.DrvFile)
	if err != nil {
		return 0, fmt.Errorf("there is an error in query: %v", err)
	}
	defer row.Close()

	var count int64

	row.Next() // no need to check return value

	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("couldn't get file count: %v", err)
	}

	return count, nil
}

func (c *Client) GetTotalSize() (int64, error) {
	query, err := c.db.Prepare("SELECT sum(size) FROM files where type=?")
	if err != nil {
		return 0, fmt.Errorf("couldn't prepare statement: %v", err)
	}

	row, err := query.Query(common.DrvFile)
	if err != nil {
		return 0, fmt.Errorf("there is an error in query: %v", err)
	}
	defer row.Close()

	var size int64

	row.Next() // no need to check return value

	if err := row.Scan(&size); err != nil {
		return 0, fmt.Errorf("couldn't get sum of sizes: %v", err)
	}

	return size, nil
}

// Insert inserts metadata to database
func (c *Client) Insert(md *Metadata) error {
	query, err := c.db.Prepare("INSERT INTO files(name, url, size, mode, parent, type, hash) VALUES(?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("couldn't prepare statement: %v", err)
	}

	if _, err := query.Exec(md.Name, md.URL, md.Size, md.Mode, md.Parent, md.Type, md.Hash); err != nil {
		return fmt.Errorf("couldn't insert file: %v", err)
	}

	return nil
}

// ForceInsert inserts metadata with provided inode, doesn't rely on autoincrement
func (c *Client) ForceInsert(md *Metadata) error {
	query, err := c.db.Prepare("INSERT INTO files(inode, name, url, size, mode, parent, type, hash) VALUES(?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("couldn't prepare statement: %v", err)
	}

	if _, err := query.Exec(md.Inode, md.Name, md.URL, md.Size, md.Mode, md.Parent, md.Type, md.Hash); err != nil {
		return fmt.Errorf("couldn't insert file: %v", err)
	}

	return nil
}

func (c *Client) fillNLink(md *Metadata) error {
	if md.Type == common.DrvFile {
		md.NLink = 1
		return nil
	}

	query, err := c.db.Prepare("SELECT COUNT(*) FROM files WHERE parent=? and type=?")
	if err != nil {
		return fmt.Errorf("couldn't prepare statement: %v", err)
	}

	row, err := query.Query(md.Inode, common.DrvFolder)
	if err != nil {
		return fmt.Errorf("there is an error in query: %v", err)
	}
	defer row.Close()

	row.Next() // should always be true

	var count int

	err = row.Scan(&count)
	if err != nil {
		return fmt.Errorf("couldn't parse row count")
	}

	// don't forget '.' and '..' dirs
	md.NLink = count + 2
	return nil
}

func (c *Client) parseRow(row *sql.Rows) (*Metadata, error) {
	md := &Metadata{}
	err := row.Scan(&md.Inode, &md.Name, &md.URL, &md.Size, &md.Mode, &md.Parent, &md.Type, &md.Hash)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse row: %v", err)
	}

	return md, nil
}
