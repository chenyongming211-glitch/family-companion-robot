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
	SourceRecordID       string `json:"sourceRecordId,omitempty"`
	SourceTurnID         string `json:"sourceTurnId,omitempty"`
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
type ChatSourceDTO struct {
	RecordID  string `json:"recordId"`
	SessionID string `json:"sessionId"`
	StartedAt string `json:"startedAt"`
}
type ConversationDTO struct {
	StartedAt       string          `json:"startedAt,omitempty"`
	LastActivityAt  string          `json:"lastActivityAt,omitempty"`
	EndedAt         string          `json:"endedAt,omitempty"`
	ArchiveReason   string          `json:"archiveReason,omitempty"`
	TurnCount       int             `json:"turnCount,omitempty"`
	ConnectionCount int             `json:"connectionCount,omitempty"`
	Sources         []ChatSourceDTO `json:"sources,omitempty"`
	ID              string          `json:"id"`
	DeviceID        string          `json:"deviceId"`
	FamilyID        string          `json:"familyId"`
	Title           string          `json:"title"`
	Date            string          `json:"date"`
	Time            string          `json:"time"`
	Status          string          `json:"status"`
	EndReason       string          `json:"endReason"`
	Summary         string          `json:"summary"`
	BodyAvailable   bool            `json:"bodyAvailable"`
	Turns           []TurnDTO       `json:"turns"`
	SourceKind      string          `json:"sourceKind,omitempty"`
	RecordStatus    string          `json:"recordStatus,omitempty"`
}
type ConversationFilter struct {
	DeviceID      *string
	Offset, Limit int
}
type ConversationPage struct {
	PendingSources bool              `json:"pendingSources,omitempty"`
	Items          []ConversationDTO `json:"items"`
	HasMore        bool              `json:"hasMore"`
}
type Reader struct {
	Config       Config
	Root         string
	ProfilesPath string
	Now          func() time.Time
}

type MemoryDTO struct {
	ID             string `json:"id"`
	DeviceID       string `json:"deviceId"`
	ConversationID string `json:"conversationId"`
	Text           string `json:"text"`
	Date           string `json:"date"`
	User           string `json:"user,omitempty"`
	Assistant      string `json:"assistant,omitempty"`
	TextSource     string `json:"textSource,omitempty"`
	Boundary       string `json:"boundary,omitempty"`
}
