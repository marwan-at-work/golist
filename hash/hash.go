package hash

import (
	"encoding/base64"
	"encoding/json"
	"marwan.io/golist/driver"
)

// Key returns a hashed representation of a config struct
func Key(cfg *driver.Config) []byte {
	return []byte(KeyString(cfg))
}

// KeyString is used because Go maps can't have []byte as keys
func KeyString(cfg *driver.Config) string {
	bts, _ := json.Marshal(cfg) // TODO: report error
	return base64.StdEncoding.EncodeToString(bts)
}

// Parse takes an encoded key and returns it a Config.
func Parse(key []byte) *driver.Config {
	var cfg driver.Config
	bts, err := base64.StdEncoding.DecodeString(string(key))
	if err != nil {
		panic("bad file put into db: " + string(key))
	}
	err = json.Unmarshal(bts, &cfg)
	if err != nil {
		panic("bad json put into db: " + string(bts))
	}
	return &cfg
}
