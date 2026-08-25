package appleads

import (
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"time"
)

type ID string

type Money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type Date string
type Timestamp string

var (
	decimalPattern  = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

func (m Money) Validate() error {
	if !decimalPattern.MatchString(m.Amount) {
		return errors.New("money amount must be a decimal string")
	}
	if !currencyPattern.MatchString(m.Currency) {
		return errors.New("money currency must be a three-letter ISO 4217 code")
	}
	return nil
}

func (m Money) ValidatePositive() error {
	if err := m.Validate(); err != nil {
		return err
	}
	value, ok := new(big.Rat).SetString(m.Amount)
	if !ok || value.Sign() <= 0 {
		return errors.New("money amount must be greater than zero")
	}
	return nil
}

func (d *Date) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("date must be a YYYY-MM-DD string")
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return errors.New("date must use YYYY-MM-DD")
	}
	*d = Date(value)
	return nil
}

func (t *Timestamp) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("timestamp must be an ISO 8601 string")
	}
	if !validTimestamp(value) {
		return errors.New("timestamp must use ISO 8601 with optional timezone")
	}
	*t = Timestamp(value)
	return nil
}

func validTimestamp(value string) bool {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000", "2006-01-02T15:04:05"} {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}
