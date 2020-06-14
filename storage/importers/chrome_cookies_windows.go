package importers

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	_ "github.com/mattn/go-sqlite3" // for chrome data stores
	go_dpapi "github.com/zsbaksa/goDPAPI"
)

// https://stackoverflow.com/questions/60416350/chrome-80-how-to-decode-cookies
// https://xenarmor.com/how-to-recover-saved-passwords-google-chrome/

// https://redd.it/39swuj/
// https://n8henrie.com/2014/05/decrypt-chrome-cookies-with-python/
// https://github.com/obsidianforensics/hindsight/blob/311c80ff35b735b273d69529d3e024d1b1aa2796/pyhindsight/browsers/chrome.py#L432

// https://gist.github.com/dacort/bd6a5116224c594b14db

// https://cs.chromium.org/chromium/src/components/os_crypt/os_crypt_linux.cc?q=saltysalt   // salt     "saltysalt"
// https://cs.chromium.org/chromium/src/components/os_crypt/os_crypt_linux.cc?q=peanuts     // password "peanuts"   for v10
var (
	dpapiHeader = []byte{1, 0, 0, 0, 208, 140, 157, 223, 1, 21, 209, 17, 140, 122, 0, 192, 79, 194, 151, 235}
	v10Header   = []byte("v10")
)

func FetchChromeCookies(ctx context.Context, profileName string, u *url.URL) ([]*http.Cookie, error) {
	log := ctxlogrus.Extract(ctx)

	curUser, err := user.Current()
	if err != nil {
		return nil, err
	}
	if profileName == "" {
		profileName = "Default"
	}
	dbFile := filepath.Join(curUser.HomeDir, `\AppData\Local\Google\Chrome\User Data\`, profileName, "Cookies.dup") // the file ext is to allow chrome to keep running

	db, err := sql.Open("sqlite3", dbFile+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var sqlArg []interface{}
	var sqlFmt = `SELECT name, value, path, host_key, expires_utc, is_secure, is_httponly, samesite, encrypted_value FROM cookies`
	if u != nil {
		sqlFmt += ` WHERE host_key LIKE ?`
		sqlArg = append(sqlArg, "%"+u.Hostname())
	}
	sqlFmt += ` ORDER BY LENGTH(path) DESC, creation_utc ASC`

	rows, err := db.QueryContext(ctx, sqlFmt, sqlArg...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cookies []*http.Cookie
	for rows.Next() {
		cookie, err := extractChromeCookie(ctx, rows.Scan)
		if err != nil {
			log.Errorf("extractChromeCookie: %v", err)
		}
		cookies = append(cookies, cookie)
	}

	return cookies, nil
}

func extractChromeCookie(ctx context.Context, scanner func(...interface{}) error) (cookie *http.Cookie, err error) {
	log := ctxlogrus.Extract(ctx)

	var enValue []byte
	cookie = new(http.Cookie)
	if err := scanner(&cookie.Name, &cookie.Value, &cookie.Path, &cookie.Domain,
		&cookie.RawExpires, &cookie.Secure, &cookie.HttpOnly, &cookie.SameSite,
		&enValue); err != nil {
		return nil, err
	}

	if cookie.RawExpires != "" {
		// Chromium stores its timestamps in sqlite on the Mac using the Windows Gregorian epoch
		// https://github.com/adobe/chromium/blob/master/base/time_mac.cc#L29
		// This converts it to a UNIX timestamp
		r, err := strconv.Atoi(cookie.RawExpires)
		if nil != err {
			return nil, err
		}
		if r != 0 {
			r = (r - 11644473600000000) / 1000000
		}
		cookie.Expires = time.Unix(int64(r), int64(0))
	}

	log.Debugf("%s %s %x", cookie.Domain, cookie.Name, enValue)
	if len(enValue) != 0 { // something to decrypt!
		if bytes.HasPrefix(enValue, dpapiHeader) {
			// DPAPI encrypted cookie handling
			log.Debugf("%s %s is old school", cookie.Domain, cookie.Name)

			deValue, err := go_dpapi.Decrypt(enValue)
			if err != nil {
				return nil, err
			}
			cookie.Value = string(deValue)
		}

		if bytes.HasPrefix(enValue, v10Header) {
			// AES-GCM-256 encrypted cookies handling (Chrome 80+)
			log.Debugf("%s %s is v10", cookie.Domain, cookie.Name)
			enValue = enValue[3:]

			key, err := fetchKey(ctx)
			if err != nil {
				return nil, err
			}
			block, err := aes.NewCipher(key)
			if err != nil {
				return nil, err
			}
			aesgcm, err := cipher.NewGCM(block)
			if err != nil {
				return nil, err
			}
			deValue, err := aesgcm.Open(nil,
				enValue[:aesgcm.NonceSize()],
				enValue[aesgcm.NonceSize():],
				nil)
			if err != nil {
				return nil, err
			}
			cookie.Value = string(deValue)
		}
	}
	return cookie, nil
}

func fetchKey(ctx context.Context) ([]byte, error) {
	curUser, err := user.Current()
	if err != nil {
		return nil, err
	}
	lsFile := filepath.Join(curUser.HomeDir, `\AppData\Local\Google\Chrome\User Data\Local State`)

	lsBytes, err := ioutil.ReadFile(lsFile)
	if err != nil {
		return nil, err
	}

	var lsStruct struct {
		OSCrypt struct {
			EncryptedKey []byte `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if err := json.Unmarshal(lsBytes, &lsStruct); err != nil {
		return nil, err
	}

	// strip leading "DPAPI" before decrypting
	return go_dpapi.Decrypt(lsStruct.OSCrypt.EncryptedKey[5:])
}
