package lister

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"
)

// New returns a new DB interface, implemented by boltDB.
func New(path string, lggr *logrus.Logger) (Service, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("could not open DB: %v", err)
	}
	if lggr == nil {
		lggr = logrus.New()
		lggr.SetLevel(logrus.DebugLevel)
	}

	return &service{db: db, lggr: lggr}, nil
}

// Service abstracts a way to cache "go list" results
type Service interface {
	Get(dir string, args []string) ([]byte, error)
	Update(dir string, args []string) error
	UpdateAll() error
}

type service struct {
	db   *bolt.DB
	lggr *logrus.Logger
}

func (c *service) Get(dir string, args []string) ([]byte, error) {
	key := getKey(args)
	var resp []byte
	err := c.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(dir))
		if err != nil {
			return fmt.Errorf("could not create bucket for %v: %v", dir, err)
		}
		bts := b.Get(key)
		if bts == nil {
			c.lggr.Debugf("running first go list for %v", dir)
			bts, err = runGoList(args, dir)
			if err != nil {
				return err
			}
			err = b.Put(key, bts)
			if err != nil {
				return fmt.Errorf("could not persist go list to boltdb: %v", err)
			}
		} else {
			c.lggr.Debugf("%v is cached", dir)
		}
		resp = bts

		return nil
	})

	return resp, err
}

func (c *service) Update(dir string, args []string) error {
	key := getKey(args)
	return c.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(dir))
		if err != nil {
			return fmt.Errorf("could not create bucket: %v", err)
		}
		bts, err := runGoList(args, dir)
		if err != nil {
			return err
		}
		return b.Put(key, bts)
	})
}
func (c *service) UpdateAll() error {
	return c.db.Update(func(tx *bolt.Tx) error {
		cur := tx.Cursor()
		var num int
		for dir, _ := cur.First(); dir != nil; dir, _ = cur.Next() {
			b := tx.Bucket([]byte(dir))
			cur := b.Cursor()
			for k, _ := cur.First(); k != nil; k, _ = cur.Next() {
				c.lggr.Debug("updating " + string(k) + " inside" + string(dir))
				args := getArgs(k)
				bts, err := runGoList(args, string(dir))
				if err != nil {
					c.lggr.Error(err)
					return err
				}
				num++
				err = b.Put(k, bts)
				if err != nil {
					c.lggr.Error(err)
					return err
				}
			}
		}

		c.lggr.Debugf("update all complete: ran go list %v times", num)
		return nil
	})
}

func runGoList(args []string, dir string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	bts, err := cmd.Output()
	if err != nil {
		// TODO: recover from this error
		return nil, fmt.Errorf("go list failed: %v", err)
	}
	return bts, nil
}

func getKey(args []string) []byte {
	return []byte(strings.Join(args, "__x__"))
}

func getArgs(key []byte) []string {
	return strings.Split(string(key), "__x__")
}
