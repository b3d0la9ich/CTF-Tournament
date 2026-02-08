package internal

type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Points   int    `json:"points"`
}

type Match struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Mode   string `json:"mode"`   // solo|team
	Status string `json:"status"` // open|closed|finished
}

type Team struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	IsOpen bool   `json:"is_open"`
}

type Application struct {
	ID      int   `json:"id"`
	MatchID int   `json:"match_id"`
	UserID  int   `json:"user_id"`
	TeamID  *int  `json:"team_id,omitempty"`
	Status  string `json:"status"`
}
