package database

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"github.com/stretchr/testify/assert"
)

func TestMongoDB_NormalizeFilterValue(t *testing.T) {
	conn := &MongoDBConn{}

	// 1. ISODate("...") should be converted to time.Time
	v := conn.normalizeFilterValue("update_dt", `ISODate("2019-06-01T12:00:00.000Z")`)
	tm, ok := v.(time.Time)
	assert.True(t, ok, "expected time.Time for ISODate")
	assert.Equal(t, 2019, tm.Year())
	assert.Equal(t, time.Month(6), tm.Month())
	assert.Equal(t, 1, tm.Day())

	// 2. ISODate(("...")) (double paren from template) should also be converted to time.Time
	v2 := conn.normalizeFilterValue("update_dt", `ISODate(("2019-06-01"))`)
	tm2, ok := v2.(time.Time)
	assert.True(t, ok, "expected time.Time for ISODate with double paren")
	assert.Equal(t, 2019, tm2.Year())

	// 3. String column with date-like string (NOT wrapped in ISODate) MUST remain a string!
	v3 := conn.normalizeFilterValue("date_str", "2019-06-01")
	str, ok := v3.(string)
	assert.True(t, ok, "expected string, not time.Time, for non-ISODate string column")
	assert.Equal(t, "2019-06-01", str)

	// 4. ObjectId("...") format
	oidHex := "507f1f77bcf86cd799439011"
	v4 := conn.normalizeFilterValue("some_field", `ObjectId("`+oidHex+`")`)
	oid, ok := v4.(primitive.ObjectID)
	assert.True(t, ok, "expected ObjectID for ObjectId constructor")
	assert.Equal(t, oidHex, oid.Hex())

	// 5. _id field with raw hex string
	v5 := conn.normalizeFilterValue("_id", oidHex)
	oid2, ok := v5.(primitive.ObjectID)
	assert.True(t, ok, "expected ObjectID for _id raw hex")
	assert.Equal(t, oidHex, oid2.Hex())

	// 6. Extended JSON $date format
	v6 := conn.normalizeFilterValue("update_dt", `{"$date": "2020-01-01T00:00:00Z"}`)
	tm3, ok := v6.(time.Time)
	assert.True(t, ok, "expected time.Time for Extended JSON $date")
	assert.Equal(t, 2020, tm3.Year())
}
