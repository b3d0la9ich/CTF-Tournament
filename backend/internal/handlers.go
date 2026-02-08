package internal

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Me(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := uid(c)
		var u User
		err := db.QueryRow(context.Background(),
			"SELECT id, username, role, points FROM users WHERE id=$1", id,
		).Scan(&u.ID, &u.Username, &u.Role, &u.Points)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		c.JSON(200, u)
	}
}

// ------------------- Rating -------------------

func Rating(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Query(context.Background(),
			"SELECT id, username, role, points FROM users ORDER BY points DESC, id ASC LIMIT 100",
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rows.Close()

		var out []User
		for rows.Next() {
			var u User
			_ = rows.Scan(&u.ID, &u.Username, &u.Role, &u.Points)
			out = append(out, u)
		}
		c.JSON(200, out)
	}
}

// ------------------- Matches (user) -------------------

// GET /api/matches?status=open|closed|finished|all
func ListMatches(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := c.Query("status")
		ctx := context.Background()

		sql := "SELECT id, title, mode, status FROM matches"
		args := []any{}
		if status != "" && status != "all" {
			sql += " WHERE status=$1"
			args = append(args, status)
		}
		sql += " ORDER BY id DESC LIMIT 200"

		rows, err := db.Query(ctx, sql, args...)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rows.Close()

		var out []Match
		for rows.Next() {
			var m Match
			_ = rows.Scan(&m.ID, &m.Title, &m.Mode, &m.Status)
			out = append(out, m)
		}
		c.JSON(200, out)
	}
}

// GET /api/my/applications => map[match_id]status
func MyApplications(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		ctx := context.Background()

		rows, err := db.Query(ctx,
			"SELECT match_id, status FROM applications WHERE user_id=$1",
			userID,
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rows.Close()

		out := map[int]string{}
		for rows.Next() {
			var mid int
			var st string
			_ = rows.Scan(&mid, &st)
			out[mid] = st
		}
		c.JSON(200, out)
	}
}

// POST /api/matches/:id/apply  (для team матчей нужен team_id)
func ApplyToMatch(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		matchID, _ := strconv.Atoi(c.Param("id"))

		var req struct {
			TeamID *int `json:"team_id"`
		}
		_ = c.BindJSON(&req)

		ctx := context.Background()

		var mode, status string
		err := db.QueryRow(ctx,
			"SELECT mode, status FROM matches WHERE id=$1",
			matchID,
		).Scan(&mode, &status)
		if err != nil {
			c.JSON(404, gin.H{"error": "match not found"})
			return
		}
		if status != "open" {
			c.JSON(400, gin.H{"error": "match is not open"})
			return
		}

		if mode == "team" {
			if req.TeamID == nil {
				c.JSON(400, gin.H{"error": "team_id is required for team match"})
				return
			}
			var ok bool
			_ = db.QueryRow(ctx,
				"SELECT EXISTS(SELECT 1 FROM team_members WHERE team_id=$1 AND user_id=$2)",
				*req.TeamID, userID,
			).Scan(&ok)
			if !ok {
				c.JSON(403, gin.H{"error": "you are not a member of this team"})
				return
			}
		} else {
			req.TeamID = nil
		}

		_, err = db.Exec(ctx,
			`INSERT INTO applications(match_id, user_id, team_id)
			 VALUES ($1,$2,$3)
			 ON CONFLICT(match_id,user_id) DO NOTHING`,
			matchID, userID, req.TeamID,
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}

		logAction(db, &userID, "apply_match", "match_id="+strconv.Itoa(matchID))
		c.JSON(200, gin.H{"ok": true})
	}
}

// GET /api/history (участие через match_participants)
func MyHistory(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		rows, err := db.Query(context.Background(),
			`SELECT m.id, m.title, m.mode, m.status
			 FROM match_participants mp
			 JOIN matches m ON m.id=mp.match_id
			 WHERE mp.user_id=$1
			 ORDER BY m.id DESC`, userID)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rows.Close()

		var out []Match
		for rows.Next() {
			var m Match
			_ = rows.Scan(&m.ID, &m.Title, &m.Mode, &m.Status)
			out = append(out, m)
		}
		c.JSON(200, out)
	}
}

// ------------------- Teams (user) -------------------

func CreateTeam(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		var req struct {
			Name   string `json:"name"`
			IsOpen bool   `json:"is_open"`
		}
		if err := c.BindJSON(&req); err != nil || req.Name == "" {
			c.JSON(400, gin.H{"error": "bad request"})
			return
		}
		var teamID int
		err := db.QueryRow(context.Background(),
			"INSERT INTO teams(name, owner_id, is_open) VALUES ($1,$2,$3) RETURNING id",
			req.Name, userID, req.IsOpen,
		).Scan(&teamID)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		_, _ = db.Exec(context.Background(),
			"INSERT INTO team_members(team_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING",
			teamID, userID,
		)
		logAction(db, &userID, "create_team", "team_id="+strconv.Itoa(teamID))
		c.JSON(200, gin.H{"ok": true, "team_id": teamID})
	}
}

func ListOpenTeams(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Query(context.Background(),
			"SELECT id, name, is_open FROM teams WHERE is_open=true ORDER BY id DESC",
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rows.Close()

		var out []Team
		for rows.Next() {
			var t Team
			_ = rows.Scan(&t.ID, &t.Name, &t.IsOpen)
			out = append(out, t)
		}
		c.JSON(200, out)
	}
}

func MyTeams(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		ctx := context.Background()

		rows, err := db.Query(ctx, `
			SELECT t.id, t.name, t.is_open
			FROM team_members tm
			JOIN teams t ON t.id = tm.team_id
			WHERE tm.user_id = $1
			ORDER BY t.name ASC
		`, userID)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rows.Close()

		var out []Team
		for rows.Next() {
			var t Team
			_ = rows.Scan(&t.ID, &t.Name, &t.IsOpen)
			out = append(out, t)
		}
		c.JSON(200, out)
	}
}

func JoinTeam(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		teamID, _ := strconv.Atoi(c.Param("id"))

		var isOpen bool
		if err := db.QueryRow(context.Background(),
			"SELECT is_open FROM teams WHERE id=$1", teamID,
		).Scan(&isOpen); err != nil || !isOpen {
			c.JSON(403, gin.H{"error": "team closed or not found"})
			return
		}
		_, err := db.Exec(context.Background(),
			"INSERT INTO team_members(team_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING",
			teamID, userID,
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		logAction(db, &userID, "join_team", "team_id="+strconv.Itoa(teamID))
		c.JSON(200, gin.H{"ok": true})
	}
}

func LeaveTeam(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		teamID, _ := strconv.Atoi(c.Param("id"))

		_, err := db.Exec(context.Background(),
			"DELETE FROM team_members WHERE team_id=$1 AND user_id=$2",
			teamID, userID,
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		logAction(db, &userID, "leave_team", "team_id="+strconv.Itoa(teamID))
		c.JSON(200, gin.H{"ok": true})
	}
}

// ------------------- Admin: logs/users -------------------

func AdminLogs(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Query(context.Background(),
			`SELECT l.id,
			        to_char(l.created_at, 'YYYY-MM-DD HH24:MI:SS') AS created_at,
			        COALESCE(u.username,'(deleted)') AS actor,
			        l.action,
			        l.details
			 FROM logs l
			 LEFT JOIN users u ON u.id=l.actor_id
			 ORDER BY l.id DESC LIMIT 200`)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rows.Close()

		type row struct {
			ID        int64  `json:"id"`
			CreatedAt string `json:"created_at"`
			Actor     string `json:"actor"`
			Action    string `json:"action"`
			Details   string `json:"details"`
		}

		out := []row{}
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.ID, &r.CreatedAt, &r.Actor, &r.Action, &r.Details); err != nil {
				c.JSON(500, gin.H{"error": "scan"})
				return
			}
			out = append(out, r)
		}

		c.JSON(200, out)
	}
}

func AdminUsers(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Query(context.Background(),
			"SELECT id, username, role, points FROM users ORDER BY id ASC",
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rows.Close()

		var out []User
		for rows.Next() {
			var u User
			_ = rows.Scan(&u.ID, &u.Username, &u.Role, &u.Points)
			out = append(out, u)
		}
		c.JSON(200, out)
	}
}

func AdminDeleteUser(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		id, _ := strconv.Atoi(c.Param("id"))
		if id == actor {
			c.JSON(400, gin.H{"error": "cannot delete yourself"})
			return
		}
		_, err := db.Exec(context.Background(), "DELETE FROM users WHERE id=$1", id)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		logAction(db, &actor, "admin_delete_user", "user_id="+strconv.Itoa(id))
		c.JSON(200, gin.H{"ok": true})
	}
}

func AdminSetPoints(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		id, _ := strconv.Atoi(c.Param("id"))
		var req struct {
			Points int `json:"points"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "bad request"})
			return
		}
		_, err := db.Exec(context.Background(),
			"UPDATE users SET points=$1 WHERE id=$2",
			req.Points, id,
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		logAction(db, &actor, "admin_set_points", "user_id="+strconv.Itoa(id))
		c.JSON(200, gin.H{"ok": true})
	}
}

// ------------------- Admin: matches CRUD -------------------

func AdminCreateMatch(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		var req struct {
			Title string `json:"title"`
			Mode  string `json:"mode"` // solo|team
		}
		if err := c.BindJSON(&req); err != nil || req.Title == "" || (req.Mode != "solo" && req.Mode != "team") {
			c.JSON(400, gin.H{"error": "bad request"})
			return
		}
		_, err := db.Exec(context.Background(),
			"INSERT INTO matches(title, mode, status, created_by) VALUES ($1,$2,'open',$3)",
			req.Title, req.Mode, actor,
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		logAction(db, &actor, "admin_create_match", req.Title)
		c.JSON(200, gin.H{"ok": true})
	}
}

func AdminUpdateMatch(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		id, _ := strconv.Atoi(c.Param("id"))
		var req struct {
			Title  string `json:"title"`
			Mode   string `json:"mode"`
			Status string `json:"status"` // open|closed|finished
		}
		if err := c.BindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "bad request"})
			return
		}
		_, err := db.Exec(context.Background(),
			"UPDATE matches SET title=$1, mode=$2, status=$3 WHERE id=$4",
			req.Title, req.Mode, req.Status, id,
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		logAction(db, &actor, "admin_update_match", "match_id="+strconv.Itoa(id))
		c.JSON(200, gin.H{"ok": true})
	}
}

func AdminDeleteMatch(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		id, _ := strconv.Atoi(c.Param("id"))
		_, err := db.Exec(context.Background(), "DELETE FROM matches WHERE id=$1", id)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		logAction(db, &actor, "admin_delete_match", "match_id="+strconv.Itoa(id))
		c.JSON(200, gin.H{"ok": true})
	}
}

func AdminListMatches(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := c.Query("status") // open|closed|finished|all
		ctx := context.Background()

		sql := "SELECT id, title, mode, status FROM matches"
		args := []any{}
		if status != "" && status != "all" {
			sql += " WHERE status=$1"
			args = append(args, status)
		}
		sql += " ORDER BY id DESC LIMIT 500"

		rows, err := db.Query(ctx, sql, args...)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rows.Close()

		var out []Match
		for rows.Next() {
			var m Match
			_ = rows.Scan(&m.ID, &m.Title, &m.Mode, &m.Status)
			out = append(out, m)
		}
		c.JSON(200, out)
	}
}

func AdminCloseMatch(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		matchID, _ := strconv.Atoi(c.Param("id"))
		ctx := context.Background()

		var status string
		if err := db.QueryRow(ctx, "SELECT status FROM matches WHERE id=$1", matchID).Scan(&status); err != nil {
			c.JSON(404, gin.H{"error": "match not found"})
			return
		}
		if status == "finished" {
			c.JSON(400, gin.H{"error": "match already finished"})
			return
		}

		_, err := db.Exec(ctx, "UPDATE matches SET status='closed' WHERE id=$1", matchID)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}

		logAction(db, &actor, "admin_close_match", "match_id="+strconv.Itoa(matchID))
		c.JSON(200, gin.H{"ok": true})
	}
}

// ------------------- Admin: applications approve/reject -------------------

func AdminListApplications(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Query(context.Background(),
			"SELECT id, match_id, user_id, team_id, status FROM applications ORDER BY id DESC LIMIT 200",
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rows.Close()

		var out []Application
		for rows.Next() {
			var a Application
			_ = rows.Scan(&a.ID, &a.MatchID, &a.UserID, &a.TeamID, &a.Status)
			out = append(out, a)
		}
		c.JSON(200, out)
	}
}

func AdminApproveApplication(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		appID, _ := strconv.Atoi(c.Param("id"))
		ctx := context.Background()

		var a Application
		err := db.QueryRow(ctx,
			"SELECT id, match_id, user_id, team_id, status FROM applications WHERE id=$1",
			appID,
		).Scan(&a.ID, &a.MatchID, &a.UserID, &a.TeamID, &a.Status)
		if err != nil {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}

		_, err = db.Exec(ctx, "UPDATE applications SET status='approved' WHERE id=$1", appID)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}

		_, _ = db.Exec(ctx,
			"INSERT INTO match_participants(match_id, user_id, team_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING",
			a.MatchID, a.UserID, a.TeamID,
		)

		logAction(db, &actor, "admin_approve_application", "app_id="+strconv.Itoa(appID))
		c.JSON(200, gin.H{"ok": true})
	}
}

func AdminRejectApplication(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		appID, _ := strconv.Atoi(c.Param("id"))
		_, err := db.Exec(context.Background(),
			"UPDATE applications SET status='rejected' WHERE id=$1",
			appID,
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		logAction(db, &actor, "admin_reject_application", "app_id="+strconv.Itoa(appID))
		c.JSON(200, gin.H{"ok": true})
	}
}

// ------------------- Admin: participants for dropdown -------------------

func AdminMatchParticipants(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		matchID, _ := strconv.Atoi(c.Param("id"))
		ctx := context.Background()

		var m Match
		err := db.QueryRow(ctx,
			"SELECT id, title, mode, status FROM matches WHERE id=$1",
			matchID,
		).Scan(&m.ID, &m.Title, &m.Mode, &m.Status)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
			return
		}

		type UserMini struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
			Points   int    `json:"points"`
		}

		rowsU, err := db.Query(ctx, `
			SELECT DISTINCT u.id, u.username, u.points
			FROM match_participants mp
			JOIN users u ON u.id = mp.user_id
			WHERE mp.match_id = $1
			ORDER BY u.username ASC
		`, matchID)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rowsU.Close()

		var users []UserMini
		for rowsU.Next() {
			var u UserMini
			_ = rowsU.Scan(&u.ID, &u.Username, &u.Points)
			users = append(users, u)
		}

		type TeamMini struct {
			ID      int        `json:"id"`
			Name    string     `json:"name"`
			Members []UserMini `json:"members"`
		}

		rowsT, err := db.Query(ctx, `
			SELECT DISTINCT t.id, t.name, u.id, u.username, u.points
			FROM match_participants mp
			JOIN teams t ON t.id = mp.team_id
			JOIN users u ON u.id = mp.user_id
			WHERE mp.match_id = $1 AND mp.team_id IS NOT NULL
			ORDER BY t.name ASC, u.username ASC
		`, matchID)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rowsT.Close()

		teamMap := map[int]*TeamMini{}
		order := []int{}
		for rowsT.Next() {
			var tid int
			var tname string
			var u UserMini
			_ = rowsT.Scan(&tid, &tname, &u.ID, &u.Username, &u.Points)

			t, ok := teamMap[tid]
			if !ok {
				t = &TeamMini{ID: tid, Name: tname}
				teamMap[tid] = t
				order = append(order, tid)
			}
			t.Members = append(t.Members, u)
		}

		var teams []TeamMini
		for _, tid := range order {
			teams = append(teams, *teamMap[tid])
		}

		c.JSON(200, gin.H{
			"match": m,
			"users": users,
			"teams": teams,
		})
	}
}

// ------------------- Admin: manual winner (team => all members) -------------------

func AdminSetWinner(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		matchID, _ := strconv.Atoi(c.Param("id"))

		var req struct {
			WinnerUserID *int `json:"winner_user_id"`
			WinnerTeamID *int `json:"winner_team_id"`
			BonusPoints  int  `json:"bonus_points"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "bad request"})
			return
		}

		// ровно один победитель
		if (req.WinnerUserID == nil && req.WinnerTeamID == nil) ||
			(req.WinnerUserID != nil && req.WinnerTeamID != nil) {
			c.JSON(400, gin.H{"error": "choose exactly one winner: winner_user_id OR winner_team_id"})
			return
		}
		if req.BonusPoints < 0 {
			c.JSON(400, gin.H{"error": "bonus_points must be >= 0"})
			return
		}

		ctx := context.Background()
		tx, err := db.Begin(ctx)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer tx.Rollback(ctx)

		var status string
		if err := tx.QueryRow(ctx, "SELECT status FROM matches WHERE id=$1", matchID).Scan(&status); err != nil {
			c.JSON(404, gin.H{"error": "match not found"})
			return
		}
		if status == "finished" {
			c.JSON(400, gin.H{"error": "match already finished"})
			return
		}

		// победитель должен быть участником
		if req.WinnerUserID != nil {
			var ok bool
			_ = tx.QueryRow(ctx,
				"SELECT EXISTS(SELECT 1 FROM match_participants WHERE match_id=$1 AND user_id=$2)",
				matchID, *req.WinnerUserID,
			).Scan(&ok)
			if !ok {
				c.JSON(400, gin.H{"error": "winner_user_id is not a participant of this match"})
				return
			}
		} else {
			var ok bool
			_ = tx.QueryRow(ctx,
				"SELECT EXISTS(SELECT 1 FROM match_participants WHERE match_id=$1 AND team_id=$2)",
				matchID, *req.WinnerTeamID,
			).Scan(&ok)
			if !ok {
				c.JSON(400, gin.H{"error": "winner_team_id is not a participant of this match"})
				return
			}
		}

		_, err = tx.Exec(ctx,
			"UPDATE matches SET status='finished', winner_user_id=$1, winner_team_id=$2 WHERE id=$3",
			req.WinnerUserID, req.WinnerTeamID, matchID,
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}

		if req.BonusPoints > 0 {
			if req.WinnerUserID != nil {
				_, err = tx.Exec(ctx,
					"UPDATE users SET points = points + $1 WHERE id=$2",
					req.BonusPoints, *req.WinnerUserID,
				)
			} else {
				// всем членам команды
				_, err = tx.Exec(ctx,
					`UPDATE users u
					 SET points = points + $1
					 FROM team_members tm
					 WHERE tm.team_id = $2 AND tm.user_id = u.id`,
					req.BonusPoints, *req.WinnerTeamID,
				)
			}
			if err != nil {
				c.JSON(500, gin.H{"error": "db"})
				return
			}
		}

		if err := tx.Commit(ctx); err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}

		logAction(db, &actor, "admin_set_winner", "match_id="+strconv.Itoa(matchID))
		c.JSON(200, gin.H{"ok": true})
	}
}

// ------------------- Admin: report (text) incl. applications -------------------

func AdminMatchReport(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		matchID, _ := strconv.Atoi(c.Param("id"))
		ctx := context.Background()

		var title, mode, status string
		var winnerUserID *int
		var winnerTeamID *int

		err := db.QueryRow(ctx,
			"SELECT title, mode, status, winner_user_id, winner_team_id FROM matches WHERE id=$1",
			matchID,
		).Scan(&title, &mode, &status, &winnerUserID, &winnerTeamID)
		if err != nil {
			c.JSON(404, gin.H{"error": "match not found"})
			return
		}

		// заявки
		type appRow struct {
			id       int
			username string
			status   string
			teamName *string
		}
		rowsA, err := db.Query(ctx, `
			SELECT a.id, u.username, a.status, t.name
			FROM applications a
			JOIN users u ON u.id=a.user_id
			LEFT JOIN teams t ON t.id=a.team_id
			WHERE a.match_id=$1
			ORDER BY a.id ASC
		`, matchID)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rowsA.Close()

		var apps []appRow
		for rowsA.Next() {
			var r appRow
			_ = rowsA.Scan(&r.id, &r.username, &r.status, &r.teamName)
			apps = append(apps, r)
		}

		// участники (факт)
		type urow struct {
			id       int
			username string
			points   int
		}
		rowsU, err := db.Query(ctx, `
			SELECT DISTINCT u.id, u.username, u.points
			FROM match_participants mp
			JOIN users u ON u.id=mp.user_id
			WHERE mp.match_id=$1
			ORDER BY u.username ASC`, matchID)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rowsU.Close()

		var users []urow
		for rowsU.Next() {
			var r urow
			_ = rowsU.Scan(&r.id, &r.username, &r.points)
			users = append(users, r)
		}

		// команды (если team)
		type trow struct {
			id      int
			name    string
			members []urow
		}
		teamMap := map[int]*trow{}
		order := []int{}

		rowsT, err := db.Query(ctx, `
			SELECT DISTINCT t.id, t.name, u.id, u.username, u.points
			FROM match_participants mp
			JOIN teams t ON t.id=mp.team_id
			JOIN users u ON u.id=mp.user_id
			WHERE mp.match_id=$1 AND mp.team_id IS NOT NULL
			ORDER BY t.name ASC, u.username ASC`, matchID)
		if err != nil {
			c.JSON(500, gin.H{"error": "db"})
			return
		}
		defer rowsT.Close()

		for rowsT.Next() {
			var tid int
			var tname string
			var ur urow
			_ = rowsT.Scan(&tid, &tname, &ur.id, &ur.username, &ur.points)
			t, ok := teamMap[tid]
			if !ok {
				t = &trow{id: tid, name: tname}
				teamMap[tid] = t
				order = append(order, tid)
			}
			t.members = append(t.members, ur)
		}

		// winner string
		winnerStr := "не определён"
		if winnerUserID != nil {
			var un string
			_ = db.QueryRow(ctx, "SELECT username FROM users WHERE id=$1", *winnerUserID).Scan(&un)
			if un != "" {
				winnerStr = "USER: " + un + " (id=" + strconv.Itoa(*winnerUserID) + ")"
			} else {
				winnerStr = "USER id=" + strconv.Itoa(*winnerUserID)
			}
		}
		if winnerTeamID != nil {
			var tn string
			_ = db.QueryRow(ctx, "SELECT name FROM teams WHERE id=$1", *winnerTeamID).Scan(&tn)
			if tn != "" {
				winnerStr = "TEAM: " + tn + " (id=" + strconv.Itoa(*winnerTeamID) + ")"
			} else {
				winnerStr = "TEAM id=" + strconv.Itoa(*winnerTeamID)
			}
		}

		report := ""
		report += "ОТЧЁТ ПО МАТЧУ CTF\n"
		report += "Матч: #" + strconv.Itoa(matchID) + " — " + title + "\n"
		report += "Режим: " + mode + "\n"
		report += "Статус: " + status + "\n"
		report += "Победитель: " + winnerStr + "\n\n"

		report += "Заявки:\n"
		if len(apps) == 0 {
			report += "- нет заявок\n\n"
		} else {
			for _, a := range apps {
				if a.teamName != nil {
					report += "- #" + strconv.Itoa(a.id) + " " + a.username + " | team=" + *a.teamName + " | " + a.status + "\n"
				} else {
					report += "- #" + strconv.Itoa(a.id) + " " + a.username + " | " + a.status + "\n"
				}
			}
			report += "\n"
		}

		if mode == "solo" {
			report += "Участники (" + strconv.Itoa(len(users)) + "):\n"
			for _, u := range users {
				report += "- " + u.username + " (id=" + strconv.Itoa(u.id) + ", points=" + strconv.Itoa(u.points) + ")\n"
			}
		} else {
			report += "Команды-участники (" + strconv.Itoa(len(order)) + "):\n"
			for _, tid := range order {
				t := teamMap[tid]
				report += "- " + t.name + " (id=" + strconv.Itoa(t.id) + "), участников: " + strconv.Itoa(len(t.members)) + "\n"
				for _, u := range t.members {
					report += "  * " + u.username + " (id=" + strconv.Itoa(u.id) + ", points=" + strconv.Itoa(u.points) + ")\n"
				}
			}
		}

		c.JSON(200, gin.H{"report": report})
	}
}
