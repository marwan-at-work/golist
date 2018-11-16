package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"
	"marwan.io/golist/driver"
	"marwan.io/golist/hash"
)

var bname = []byte("driver")

// New returns a new DB interface, implemented by boltDB.
func New(path string, lggr *logrus.Logger) (Service, error) {
	// TODO: By the time we get here, this shouldn't time out.
	db, err := bolt.Open(path, 0660, &bolt.Options{
		Timeout: time.Second * 5,
	})
	if err != nil {
		return nil, fmt.Errorf("could not open DB: %v", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bname)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("could not create driver bucket: %v", err)
	}
	if lggr == nil {
		lggr = logrus.New()
		lggr.SetLevel(logrus.DebugLevel)
	}

	return &service{db: db, lggr: lggr}, nil
}

// Service abstracts a way to cache go/packages results
type Service interface {
	Get(ctx context.Context, cfg *driver.Config) ([]byte, error)
	Update(ctx context.Context, cfg *driver.Config) error
	UpdateAll(ctx context.Context) error
	Close() error
}

type service struct {
	db   *bolt.DB
	lggr *logrus.Logger
}

func (c *service) Get(ctx context.Context, cfg *driver.Config) ([]byte, error) {
	key := hash.Key(cfg)
	var resp []byte
	c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bname)
		resp = b.Get(key)
		return nil
	})

	if resp != nil {
		c.lggr.Debugf("%v is already in cache", cfg.Patterns)
		return resp, nil
	}

	c.lggr.Debugf("%v is not in cache", cfg.Patterns)
	err := c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bname)
		bts := b.Get(key)
		if bts == nil {
			var err error
			c.lggr.Debugf("running first driver for %v", cfg.Patterns)
			bts, err = runDriver(ctx, cfg)
			if err != nil {
				return err
			}
			err = b.Put(key, bts)
			if err != nil {
				return fmt.Errorf("could not persist go list to boltdb: %v", err)
			}
		} else {
			c.lggr.Debugf("%v is in cache", cfg.Patterns)
		}
		resp = bts

		return nil
	})

	return resp, err
}

func (c *service) Update(ctx context.Context, cfg *driver.Config) error {
	key := hash.Key(cfg)
	return c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bname)
		bts, err := runDriver(ctx, cfg)
		if err != nil {
			return err
		}
		return b.Put(key, bts)
	})
}

func (c *service) UpdateAll(ctx context.Context) error {
	return c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bname)
		cur := b.Cursor()
		var num int
		for key, _ := cur.First(); key != nil; key, _ = cur.Next() {
			cfg := hash.Parse(key)
			c.lggr.Debugf("updating: %v", cfg.Patterns)
			bts, err := runDriver(ctx, cfg)
			if err != nil {
				c.lggr.Errorf("driver err: %v", err)
				c.lggr.Debugf("removing key: %s", key)
				b.Delete(key)
				continue
			}
			num++
			err = b.Put(key, bts)
			if err != nil {
				c.lggr.Errorf("udpate err: %v", err)
				c.lggr.Debugf("removing key: %s", key)
				b.Delete(key)
				continue
			}
		}

		c.lggr.Debugf("update all complete: ran driver %v times", num)
		return nil
	})
}

func (c *service) Close() error {
	return c.db.Close()
}

func runDriver(ctx context.Context, cfg *driver.Config) ([]byte, error) {
	dresp, err := driver.GoListDriver(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return json.Marshal(dresp)
}
