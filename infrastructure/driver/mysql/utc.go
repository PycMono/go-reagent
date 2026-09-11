package mysql

import (
	"gorm.io/gorm"
	"time"
)

// UTCNow reads text deliberately: the existing MySQL SDK fixes loc=PRC, which
// would otherwise interpret UTC_TIMESTAMP's timezone-free value eight hours early.
func UTCNow(db *gorm.DB) (time.Time, error) {
	var text string
	if err := db.Raw("SELECT CAST(UTC_TIMESTAMP(6) AS CHAR)").Scan(&text).Error; err != nil {
		return time.Time{}, err
	}
	return time.Parse("2006-01-02 15:04:05.999999", text)
}

// DecodeUTC restores the zone of DATETIME columns explicitly stored as UTC text.
// Existing conversation tables retain their original SDK timezone semantics.
func DecodeUTC(value time.Time) time.Time {
	if value.IsZero() {
		return value
	}
	return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), value.Minute(), value.Second(), value.Nanosecond(), time.UTC)
}
