package management

import "time"

type DeviceDTO struct {
	ID         string  `json:"id"`
	FamilyID   string  `json:"familyId"`
	Name       string  `json:"name"`
	Location   string  `json:"location"`
	Model      string  `json:"model"`
	Connection string  `json:"connection"`
	Activity   string  `json:"activity"`
	LastSeen   string  `json:"lastSeen"`
	Battery    *int    `json:"battery"`
	Volume     *int    `json:"volume"`
	Brightness *int    `json:"brightness"`
	Revision   *uint64 `json:"revision"`
}
type TurnDTO struct {
	ID                   string `json:"id"`
	User                 string `json:"user"`
	Played               string `json:"played"`
	Generated            string `json:"generated"`
	Playback             string `json:"playback"`
	Saved                bool   `json:"saved"`
	TextSource           string `json:"textSource,omitempty"`
	Boundary             string `json:"boundary,omitempty"`
	ExactStopKnown       bool   `json:"exactStopKnown"`
	RecognitionMayDiffer bool   `json:"recognitionMayDiffer"`
}
type ConversationDTO struct {
	ID            string    `json:"id"`
	DeviceID      string    `json:"deviceId"`
	FamilyID      string    `json:"familyId"`
	Title         string    `json:"title"`
	Date          string    `json:"date"`
	Time          string    `json:"time"`
	Status        string    `json:"status"`
	EndReason     string    `json:"endReason"`
	Summary       string    `json:"summary"`
	BodyAvailable bool      `json:"bodyAvailable"`
	Turns         []TurnDTO `json:"turns"`
	SourceKind    string    `json:"sourceKind,omitempty"`
	RecordStatus  string    `json:"recordStatus,omitempty"`
}
type ConversationFilter struct {
	DeviceID      *string
	Offset, Limit int
}
type ConversationPage struct {
	Items   []ConversationDTO `json:"items"`
	HasMore bool              `json:"hasMore"`
}
type Reader struct {
	Config       Config
	Root         string
	ProfilesPath string
	Now          func() time.Time
}
