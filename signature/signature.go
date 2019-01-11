package signature

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const tsHeader = "MessageBird-Request-Timestamp"
const sHeader = "MessageBird-Signature"

// ValidityPeriod is the time in hours after which a request is descarded
type ValidityPeriod *float64

// StringToTime converts from Unicod Epoch enconded timestamps to time.Time Go objects
func stringToTime(s string) (time.Time, error) {
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(sec, 0), nil
}

// HMACSHA256 generates HMACS enconded hashes using the provided Key and SHA256
// encoding for the message
func hMACSHA256(message, key []byte) ([]byte, error) {
	mac := hmac.New(sha256.New, []byte(key))
	if _, err := mac.Write(message); err != nil {
		return nil, err
	}
	return mac.Sum(nil), nil
}

// Validator type represents a MessageBird signature validator
type Validator struct {
	SigningKey  string         // Signing Key provided by MessageBird
	Period      ValidityPeriod // Period in hours for a message to be accepted as real, set to nil to bypass the timestamp validator
	Log         *log.Logger
	LogMesssage *string
}

// NewValidator returns a signature validator object
func NewValidator(signingKey string, period ValidityPeriod, log *log.Logger, message *string) *Validator {
	return &Validator{
		SigningKey:  signingKey,
		Period:      period,
		Log:         log,
		LogMesssage: message,
	}
}

// ValidTimestamp validates if the MessageBird-Request-Timestamp is a valid
// date and if the request is older than the validator Period.
func (v *Validator) ValidTimestamp(ts string) bool {
	t, err := stringToTime(ts)
	if err != nil {
		return false
	}
	if v.Period != nil {
		now := time.Now()
		diff := now.Sub(t)
		if math.Abs(diff.Hours()) > *v.Period {
			return false
		}
	}
	return true
}

// CalculateSignature calculates the MessageBird-Signature using HMAC_SHA_256
// encoding and the timestamp, query params and body from the request:
// signature = HMAC_SHA_256(
//	TIMESTAMP + \n + QUERY_PARAMS + \n + SHA_256_SUM(BODY),
//	signing_key)
func (v *Validator) CalculateSignature(ts, qp string, b []byte) ([]byte, error) {
	var m bytes.Buffer
	bh := sha256.Sum256(b)
	fmt.Fprintf(&m, "%s\n%s\n%s", ts, qp, bh[:])
	s, err := hMACSHA256(m.Bytes(), []byte(v.SigningKey))
	if err != nil {
		return nil, err
	}
	return s, nil
}

// ValidSignature takes the timestamp, query params and body from the request,
// calculates the expected signature and compares it to the one sent by MessageBird.
func (v *Validator) ValidSignature(ts, rqp string, b []byte, rs string) bool {
	uqp, _ := url.Parse("?" + rqp)
	es, _ := v.CalculateSignature(ts, uqp.Query().Encode(), b)
	drs, _ := base64.StdEncoding.DecodeString(rs)
	return hmac.Equal(drs, es)
}

func (v *Validator) Error(w http.ResponseWriter, r *http.Request) {
	if v.Log != nil {
		v.Log.Printf("%s, sending host: %s", *v.LogMesssage, r.Host)
	}
	http.Error(w, "Request not allowed", http.StatusUnauthorized)
	return
}

// Validate is a handler wrapper that takes care of the signature validation of
// incoming requests and rejects them if invalid or pass them on to your handler
// otherwise.
// To use just wrappe your handler with it:
// http.Handle("/path", signature.Validate(handleThing))
func (v *Validator) Validate(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts := r.Header.Get(tsHeader)
		rs := r.Header.Get(sHeader)
		if ts == "" || rs == "" {
			v.Error(w, r)
			return
		}
		b, _ := ioutil.ReadAll(r.Body)
		if v.ValidTimestamp(ts) == false || v.ValidSignature(ts, r.URL.RawQuery, b, rs) == false {
			v.Error(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}
