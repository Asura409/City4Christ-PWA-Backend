package pastoral

import "time"

type Booking struct {
	ID              string    `json:"id"`
	SlotID          string    `json:"slotId"`
	PastorID        string    `json:"pastorId"`
	MemberID        string    `json:"memberId"`
	PastorFirstName string    `json:"pastorFirstName"`
	PastorLastName  string    `json:"pastorLastName"`
	MemberFirstName string    `json:"memberFirstName"`
	MemberLastName  string    `json:"memberLastName"`
	StartsAt        time.Time `json:"startsAt"`
	EndsAt          time.Time `json:"endsAt"`
	TimeZone        string    `json:"timeZone"`
	Status          string    `json:"status"`
	Version         int       `json:"version"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}
type Decision struct {
	Status  string `json:"status"`
	Version int    `json:"version"`
}
