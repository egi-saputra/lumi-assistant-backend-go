package utils

import (
	"math/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

var entropy = ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)

func NewULID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}