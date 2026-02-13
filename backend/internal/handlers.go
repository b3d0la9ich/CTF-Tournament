package internal

import (
	"context"
	"strconv"
	"strings"

	sq "github.com/Masterminds/squirrel"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

/* ===================== LIMITS ===================== */

func clampRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return string(r)
}

func jsonErr(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"error": msg})
}

func normStatus(s string) string {
	s = clampRunes(strings.ToLower(s), MaxStatus)
	switch s {
	case "open", "closed", "finished", "all", "":
		return s
	default:
		return "invalid"
	}
}

func normMode(s string) string {
	s = clampRunes(strings.ToLower(s), MaxMode)
	switch s {
	case "solo", "team":
		return s
	default:
		return "invalid"
	}
}

/* ===================== BASIC ===================== */

func Me(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := uid(c)
		ctx := context.Background()

		var u User
		q := sq.Select("id", "username", "role", "points").
			From("users").
			Where(sq.Eq{"id": id}).
			PlaceholderFormat(sq.Dollar)

		if err := qRow(ctx, db, q).Scan(&u.ID, &u.Username, &u.Role, &u.Points); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		c.JSON(200, u)
	}
}

func userHasAnyTeam(db *pgxpool.Pool, userID int) (bool, error) {
	ctx := context.Background()

	sub := sq.Select("1").
		From("team_members").
		Where(sq.Eq{"user_id": userID}).
		PlaceholderFormat(sq.Dollar)

	q := sq.Select().
		Column(sq.Expr("EXISTS(?)", sub)).
		PlaceholderFormat(sq.Dollar)

	var ok bool
	err := qRow(ctx, db, q).Scan(&ok)
	return ok, err
}

/* ===================== RATING ===================== */

func Rating(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		q := sq.Select("id", "username", "role", "points").
			From("users").
			Where(sq.NotEq{"role": "admin"}).
			OrderBy("points DESC", "id ASC").
			Limit(100).
			PlaceholderFormat(sq.Dollar)

		rows, err := qQuery(ctx, db, q)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
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

/* ===================== MATCHES (USER) ===================== */

// GET /api/matches?status=open|closed|finished|all
func ListMatches(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := normStatus(c.Query("status"))
		if status == "invalid" {
			jsonErr(c, 400, "Некорректный статус")
			return
		}

		ctx := context.Background()

		q := sq.Select("id", "title", "mode", "status").
			From("matches").
			OrderBy("id DESC").
			Limit(200).
			PlaceholderFormat(sq.Dollar)

		if status != "" && status != "all" {
			q = q.Where(sq.Eq{"status": status})
		}

		rows, err := qQuery(ctx, db, q)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
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

		q := sq.Select("match_id", "status").
			From("applications").
			Where(sq.Eq{"user_id": userID}).
			PlaceholderFormat(sq.Dollar)

		rows, err := qQuery(ctx, db, q)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
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

// POST /api/matches/:id/apply (для team матчей нужен team_id)
func ApplyToMatch(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		matchID, _ := strconv.Atoi(c.Param("id"))
		if matchID <= 0 {
			jsonErr(c, 400, "Некорректный матч")
			return
		}

		var req struct {
			TeamID *int `json:"team_id"`
		}
		_ = c.BindJSON(&req)

		ctx := context.Background()

		var mode, status, title string
		qMatch := sq.Select("mode", "status", "title").
			From("matches").
			Where(sq.Eq{"id": matchID}).
			PlaceholderFormat(sq.Dollar)

		if err := qRow(ctx, db, qMatch).Scan(&mode, &status, &title); err != nil {
			jsonErr(c, 404, "Матч не найден")
			return
		}
		if status != "open" {
			jsonErr(c, 400, "Матч закрыт для заявок")
			return
		}

		if mode == "team" {
			if req.TeamID == nil || *req.TeamID <= 0 {
				jsonErr(c, 400, "Выберите команду")
				return
			}

			sub := sq.Select("1").
				From("team_members").
				Where(sq.Eq{"team_id": *req.TeamID, "user_id": userID}).
				PlaceholderFormat(sq.Dollar)

			qExists := sq.Select().
				Column(sq.Expr("EXISTS(?)", sub)).
				PlaceholderFormat(sq.Dollar)

			var ok bool
			_ = qRow(ctx, db, qExists).Scan(&ok)
			if !ok {
				jsonErr(c, 403, "Вы не состоите в выбранной команде")
				return
			}
		} else {
			req.TeamID = nil
		}

		ins := sq.Insert("applications").
			Columns("match_id", "user_id", "team_id").
			Values(matchID, userID, req.TeamID).
			Suffix("ON CONFLICT(match_id,user_id) DO NOTHING").
			PlaceholderFormat(sq.Dollar)

		if _, err := qExec(ctx, db, ins); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		// ✅ лог без ID и без username
		logAction(db, &userID, "apply_match", "Пользователь подал заявку на матч: "+clampRunes(title, MaxReportLine))

		c.JSON(200, gin.H{"ok": true})
	}
}

// GET /api/history
func MyHistory(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		ctx := context.Background()

		q := sq.Select("m.id", "m.title", "m.mode", "m.status").
			From("match_participants mp").
			Join("matches m ON m.id = mp.match_id").
			Where(sq.Eq{"mp.user_id": userID}).
			OrderBy("m.id DESC").
			PlaceholderFormat(sq.Dollar)

		rows, err := qQuery(ctx, db, q)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
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

/* ===================== TEAMS (USER) ===================== */

func CreateTeam(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		ctx := context.Background()

		has, err := userHasAnyTeam(db, userID)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		if has {
			jsonErr(c, 400, "Сначала выйдите из текущей команды")
			return
		}

		var req struct {
			Name   string `json:"name"`
			IsOpen bool   `json:"is_open"`
		}
		if err := c.BindJSON(&req); err != nil {
			jsonErr(c, 400, "Некорректные данные")
			return
		}

		req.Name = clampRunes(req.Name, MaxTeamName)
		if req.Name == "" {
			jsonErr(c, 400, "Введите название команды")
			return
		}

		insTeam := sq.Insert("teams").
			Columns("name", "owner_id", "is_open").
			Values(req.Name, userID, req.IsOpen).
			Suffix("RETURNING id").
			PlaceholderFormat(sq.Dollar)

		var teamID int
		if err := qRow(ctx, db, insTeam).Scan(&teamID); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		insMember := sq.Insert("team_members").
			Columns("team_id", "user_id").
			Values(teamID, userID).
			Suffix("ON CONFLICT DO NOTHING").
			PlaceholderFormat(sq.Dollar)

		_, _ = qExec(ctx, db, insMember)

		logAction(db, &userID, "create_team", "Пользователь создал команду")
		c.JSON(200, gin.H{"ok": true, "team_id": teamID})
	}
}

func ListOpenTeams(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		type UserMini struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
		}
		type TeamOut struct {
			ID      int        `json:"id"`
			Name    string     `json:"name"`
			IsOpen  bool       `json:"is_open"`
			Members []UserMini `json:"members"`
		}

		qTeams := sq.Select("id", "name", "is_open").
			From("teams").
			Where(sq.Eq{"is_open": true}).
			OrderBy("id DESC").
			PlaceholderFormat(sq.Dollar)

		rows, err := qQuery(ctx, db, qTeams)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		defer rows.Close()

		teams := make([]TeamOut, 0, 64)
		byID := make(map[int]int, 64)

		for rows.Next() {
			var t TeamOut
			_ = rows.Scan(&t.ID, &t.Name, &t.IsOpen)
			t.Members = []UserMini{}
			byID[t.ID] = len(teams)
			teams = append(teams, t)
		}

		if len(teams) == 0 {
			c.JSON(200, teams)
			return
		}

		qMembers := sq.Select("tm.team_id", "u.id", "u.username").
			From("team_members tm").
			Join("users u ON u.id = tm.user_id").
			Join("teams t ON t.id = tm.team_id").
			Where(sq.Eq{"t.is_open": true}).
			OrderBy("tm.team_id", "u.username").
			PlaceholderFormat(sq.Dollar)

		rowsM, err := qQuery(ctx, db, qMembers)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		defer rowsM.Close()

		for rowsM.Next() {
			var tid int
			var u UserMini
			_ = rowsM.Scan(&tid, &u.ID, &u.Username)
			if idx, ok := byID[tid]; ok {
				teams[idx].Members = append(teams[idx].Members, u)
			}
		}

		c.JSON(200, teams)
	}
}

func MyTeams(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		ctx := context.Background()

		type UserMini struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
		}
		type TeamOut struct {
			ID      int        `json:"id"`
			Name    string     `json:"name"`
			IsOpen  bool       `json:"is_open"`
			Members []UserMini `json:"members"`
		}

		qTeams := sq.Select("t.id", "t.name", "t.is_open").
			From("team_members tm").
			Join("teams t ON t.id = tm.team_id").
			Where(sq.Eq{"tm.user_id": userID}).
			OrderBy("t.name ASC").
			PlaceholderFormat(sq.Dollar)

		rows, err := qQuery(ctx, db, qTeams)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		defer rows.Close()

		teams := make([]TeamOut, 0, 16)
		byID := make(map[int]int, 16)

		for rows.Next() {
			var t TeamOut
			_ = rows.Scan(&t.ID, &t.Name, &t.IsOpen)
			t.Members = []UserMini{}
			byID[t.ID] = len(teams)
			teams = append(teams, t)
		}

		if len(teams) == 0 {
			c.JSON(200, teams)
			return
		}

		subMe := sq.Select("1").
			From("team_members me").
			Where(sq.Expr("me.team_id = tm.team_id")).
			Where(sq.Eq{"me.user_id": userID}).
			PlaceholderFormat(sq.Dollar)

		qMembers := sq.Select("tm.team_id", "u.id", "u.username").
			From("team_members tm").
			Join("users u ON u.id = tm.user_id").
			Where(sq.Expr("EXISTS(?)", subMe)).
			OrderBy("tm.team_id", "u.username").
			PlaceholderFormat(sq.Dollar)

		rowsM, err := qQuery(ctx, db, qMembers)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		defer rowsM.Close()

		for rowsM.Next() {
			var tid int
			var u UserMini
			_ = rowsM.Scan(&tid, &u.ID, &u.Username)
			if idx, ok := byID[tid]; ok {
				teams[idx].Members = append(teams[idx].Members, u)
			}
		}

		c.JSON(200, teams)
	}
}

func JoinTeam(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		teamID, _ := strconv.Atoi(c.Param("id"))
		if teamID <= 0 {
			jsonErr(c, 400, "Некорректная команда")
			return
		}

		ctx := context.Background()

		has, err := userHasAnyTeam(db, userID)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		if has {
			jsonErr(c, 400, "Сначала выйдите из текущей команды")
			return
		}

		qTeam := sq.Select("is_open").
			From("teams").
			Where(sq.Eq{"id": teamID}).
			PlaceholderFormat(sq.Dollar)

		var isOpen bool
		if err := qRow(ctx, db, qTeam).Scan(&isOpen); err != nil || !isOpen {
			jsonErr(c, 403, "Команда недоступна")
			return
		}

		ins := sq.Insert("team_members").
			Columns("team_id", "user_id").
			Values(teamID, userID).
			Suffix("ON CONFLICT DO NOTHING").
			PlaceholderFormat(sq.Dollar)

		if _, err := qExec(ctx, db, ins); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		logAction(db, &userID, "join_team", "Пользователь вступил в команду")
		c.JSON(200, gin.H{"ok": true})
	}
}

func LeaveTeam(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := uid(c)
		teamID, _ := strconv.Atoi(c.Param("id"))
		if teamID <= 0 {
			jsonErr(c, 400, "Некорректная команда")
			return
		}

		ctx := context.Background()

		del := sq.Delete("team_members").
			Where(sq.Eq{"team_id": teamID, "user_id": userID}).
			PlaceholderFormat(sq.Dollar)

		if _, err := qExec(ctx, db, del); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		logAction(db, &userID, "leave_team", "Пользователь вышел из команды")
		c.JSON(200, gin.H{"ok": true})
	}
}

/* ===================== ADMIN: LOGS/USERS ===================== */

func AdminLogs(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		// ✅ без ID и без username
		q := sq.Select(
			"to_char(l.created_at, 'YYYY-MM-DD HH24:MI:SS') AS created_at",
			"CASE WHEN u.role='admin' THEN 'Администратор' ELSE 'Пользователь' END AS actor",
			"l.action",
			"l.details",
		).
			From("logs l").
			LeftJoin("users u ON u.id = l.actor_id").
			OrderBy("l.id DESC").
			Limit(200).
			PlaceholderFormat(sq.Dollar)

		rows, err := qQuery(ctx, db, q)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		defer rows.Close()

		type row struct {
			CreatedAt string `json:"created_at"`
			Actor     string `json:"actor"`
			Action    string `json:"action"`
			Details   string `json:"details"`
		}

		out := []row{}
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.CreatedAt, &r.Actor, &r.Action, &r.Details); err != nil {
				jsonErr(c, 500, "Ошибка сервера")
				return
			}
			out = append(out, r)
		}

		c.JSON(200, out)
	}
}

func AdminUsers(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		q := sq.Select("id", "username", "role", "points").
			From("users").
			OrderBy("id ASC").
			PlaceholderFormat(sq.Dollar)

		rows, err := qQuery(ctx, db, q)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
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
		if id <= 0 {
			jsonErr(c, 400, "Некорректный пользователь")
			return
		}
		if id == actor {
			jsonErr(c, 400, "Нельзя удалить самого себя")
			return
		}

		ctx := context.Background()

		del := sq.Delete("users").
			Where(sq.Eq{"id": id}).
			PlaceholderFormat(sq.Dollar)

		if _, err := qExec(ctx, db, del); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		logAction(db, &actor, "admin_delete_user", "Администратор удалил пользователя")
		c.JSON(200, gin.H{"ok": true})
	}
}

func AdminSetPoints(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		id, _ := strconv.Atoi(c.Param("id"))
		if id <= 0 {
			jsonErr(c, 400, "Некорректный пользователь")
			return
		}

		var req struct {
			Points int `json:"points"`
		}
		if err := c.BindJSON(&req); err != nil {
			jsonErr(c, 400, "Некорректные данные")
			return
		}
		if req.Points < 0 || req.Points > 1_000_000 {
			jsonErr(c, 400, "Некорректное значение очков")
			return
		}

		ctx := context.Background()

		upd := sq.Update("users").
			Set("points", req.Points).
			Where(sq.Eq{"id": id}).
			PlaceholderFormat(sq.Dollar)

		if _, err := qExec(ctx, db, upd); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		logAction(db, &actor, "admin_set_points", "Администратор изменил очки пользователя")
		c.JSON(200, gin.H{"ok": true})
	}
}

/* ===================== ADMIN: MATCHES CRUD ===================== */

func AdminCreateMatch(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)

		var req struct {
			Title string `json:"title"`
			Mode  string `json:"mode"`
		}
		if err := c.BindJSON(&req); err != nil {
			jsonErr(c, 400, "Некорректные данные")
			return
		}

		req.Title = clampRunes(req.Title, MaxMatchTitle)
		req.Mode = normMode(req.Mode)
		if req.Title == "" || req.Mode == "invalid" {
			jsonErr(c, 400, "Некорректные данные")
			return
		}

		ctx := context.Background()

		ins := sq.Insert("matches").
			Columns("title", "mode", "status", "created_by").
			Values(req.Title, req.Mode, "open", actor).
			PlaceholderFormat(sq.Dollar)

		if _, err := qExec(ctx, db, ins); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		logAction(db, &actor, "admin_create_match", "Администратор создал матч: "+clampRunes(req.Title, MaxReportLine))
		c.JSON(200, gin.H{"ok": true})
	}
}

func AdminUpdateMatch(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		id, _ := strconv.Atoi(c.Param("id"))
		if id <= 0 {
			jsonErr(c, 400, "Некорректный матч")
			return
		}

		var req struct {
			Title  string `json:"title"`
			Mode   string `json:"mode"`
			Status string `json:"status"`
		}
		if err := c.BindJSON(&req); err != nil {
			jsonErr(c, 400, "Некорректные данные")
			return
		}

		req.Title = clampRunes(req.Title, MaxMatchTitle)
		req.Mode = normMode(req.Mode)
		req.Status = normStatus(req.Status)
		if req.Title == "" || req.Mode == "invalid" || req.Status == "invalid" || req.Status == "all" || req.Status == "" {
			jsonErr(c, 400, "Некорректные данные")
			return
		}

		ctx := context.Background()

		upd := sq.Update("matches").
			Set("title", req.Title).
			Set("mode", req.Mode).
			Set("status", req.Status).
			Where(sq.Eq{"id": id}).
			PlaceholderFormat(sq.Dollar)

		if _, err := qExec(ctx, db, upd); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		logAction(db, &actor, "admin_update_match", "Администратор изменил матч")
		c.JSON(200, gin.H{"ok": true})
	}
}

func AdminDeleteMatch(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		id, _ := strconv.Atoi(c.Param("id"))
		if id <= 0 {
			jsonErr(c, 400, "Некорректный матч")
			return
		}

		ctx := context.Background()

		del := sq.Delete("matches").
			Where(sq.Eq{"id": id}).
			PlaceholderFormat(sq.Dollar)

		if _, err := qExec(ctx, db, del); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		logAction(db, &actor, "admin_delete_match", "Администратор удалил матч")
		c.JSON(200, gin.H{"ok": true})
	}
}

func AdminListMatches(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := normStatus(c.Query("status"))
		if status == "invalid" {
			jsonErr(c, 400, "Некорректный статус")
			return
		}

		ctx := context.Background()

		q := sq.Select("id", "title", "mode", "status").
			From("matches").
			OrderBy("id DESC").
			Limit(500).
			PlaceholderFormat(sq.Dollar)

		if status != "" && status != "all" {
			q = q.Where(sq.Eq{"status": status})
		}

		rows, err := qQuery(ctx, db, q)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
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
		if matchID <= 0 {
			jsonErr(c, 400, "Некорректный матч")
			return
		}

		ctx := context.Background()

		qSt := sq.Select("status", "title").
			From("matches").
			Where(sq.Eq{"id": matchID}).
			PlaceholderFormat(sq.Dollar)

		var status, title string
		if err := qRow(ctx, db, qSt).Scan(&status, &title); err != nil {
			jsonErr(c, 404, "Матч не найден")
			return
		}
		if status == "finished" {
			jsonErr(c, 400, "Матч уже завершён")
			return
		}

		upd := sq.Update("matches").
			Set("status", "closed").
			Where(sq.Eq{"id": matchID}).
			PlaceholderFormat(sq.Dollar)

		if _, err := qExec(ctx, db, upd); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		logAction(db, &actor, "admin_close_match", "Администратор закрыл матч: "+clampRunes(title, MaxReportLine))
		c.JSON(200, gin.H{"ok": true})
	}
}

/* ===================== ADMIN: APPLICATIONS ===================== */

func AdminListApplications(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		q := sq.Select(
			"a.id",
			"m.title",
			"u.username",
			"COALESCE(t.name,'')",
			"a.status",
		).
			From("applications a").
			Join("matches m ON m.id = a.match_id").
			Join("users u ON u.id = a.user_id").
			LeftJoin("teams t ON t.id = a.team_id").
			OrderBy("a.id DESC").
			PlaceholderFormat(sq.Dollar)

		rows, err := qQuery(ctx, db, q)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		defer rows.Close()

		type outRow struct {
			ID        int64  `json:"id"`
			Match     string `json:"match"`
			User      string `json:"user"`
			Team      string `json:"team"`
			Status    string `json:"status"`
		}

		var out []outRow
		for rows.Next() {
			var r outRow
			_ = rows.Scan(&r.ID, &r.Match, &r.User, &r.Team, &r.Status)
			out = append(out, r)
		}
		c.JSON(200, out)
	}
}

func AdminApproveApplication(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		appID, _ := strconv.Atoi(c.Param("id"))
		if appID <= 0 {
			jsonErr(c, 400, "Некорректная заявка")
			return
		}

		ctx := context.Background()

		var matchID, userID int
		var teamID *int
		var st string
		qApp := sq.Select("match_id", "user_id", "team_id", "status").
			From("applications").
			Where(sq.Eq{"id": appID}).
			PlaceholderFormat(sq.Dollar)

		if err := qRow(ctx, db, qApp).Scan(&matchID, &userID, &teamID, &st); err != nil {
			jsonErr(c, 404, "Заявка не найдена")
			return
		}
		if strings.ToLower(st) != "pending" {
			jsonErr(c, 400, "Решение уже принято")
			return
		}

		tx, err := db.Begin(ctx)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		defer tx.Rollback(ctx)

		upd := sq.Update("applications").
			Set("status", "approved").
			Where(sq.Eq{"id": appID}).
			PlaceholderFormat(sq.Dollar)
		_, _ = qExecTx(ctx, tx, upd)

		ins := sq.Insert("match_participants").
			Columns("match_id", "user_id", "team_id").
			Values(matchID, userID, teamID).
			PlaceholderFormat(sq.Dollar)
		_, _ = qExecTx(ctx, tx, ins)

		if err := tx.Commit(ctx); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		logAction(db, &actor, "admin_approve_application", "Администратор одобрил заявку")
		c.JSON(200, gin.H{"ok": true})
	}
}

func AdminRejectApplication(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		appID, _ := strconv.Atoi(c.Param("id"))
		if appID <= 0 {
			jsonErr(c, 400, "Некорректная заявка")
			return
		}

		ctx := context.Background()

		var matchID, userID int
		var teamID *int
		var st string
		qApp := sq.Select("match_id", "user_id", "team_id", "status").
			From("applications").
			Where(sq.Eq{"id": appID}).
			PlaceholderFormat(sq.Dollar)

		if err := qRow(ctx, db, qApp).Scan(&matchID, &userID, &teamID, &st); err != nil {
			jsonErr(c, 404, "Заявка не найдена")
			return
		}
		if strings.ToLower(st) != "pending" {
			jsonErr(c, 400, "Решение уже принято")
			return
		}

		tx, err := db.Begin(ctx)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		defer tx.Rollback(ctx)

		upd := sq.Update("applications").
			Set("status", "rejected").
			Where(sq.Eq{"id": appID}).
			PlaceholderFormat(sq.Dollar)
		_, _ = qExecTx(ctx, tx, upd)

		var del sq.DeleteBuilder
		if teamID != nil {
			del = sq.Delete("match_participants").
				Where(sq.Eq{"match_id": matchID, "user_id": userID, "team_id": *teamID}).
				PlaceholderFormat(sq.Dollar)
		} else {
			del = sq.Delete("match_participants").
				Where(sq.Eq{"match_id": matchID, "user_id": userID}).
				PlaceholderFormat(sq.Dollar)
		}
		_, _ = qExecTx(ctx, tx, del)

		if err := tx.Commit(ctx); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		logAction(db, &actor, "admin_reject_application", "Администратор отклонил заявку")
		c.JSON(200, gin.H{"ok": true})
	}
}

/* ===================== ADMIN: PARTICIPANTS ===================== */

func AdminMatchParticipants(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		matchID, _ := strconv.Atoi(c.Param("id"))
		if matchID <= 0 {
			jsonErr(c, 400, "Некорректный матч")
			return
		}
		ctx := context.Background()

		// 1) режим матча (через squirrel)
		var mode string
		qMode := sq.Select("mode").
			From("matches").
			Where(sq.Eq{"id": matchID}).
			PlaceholderFormat(sq.Dollar)

		if err := qRow(ctx, db, qMode).Scan(&mode); err != nil {
			jsonErr(c, 404, "Матч не найден")
			return
		}

		out := gin.H{
			"match": gin.H{
				"id":   matchID,
				"mode": mode,
			},
		}

		// 2) SOLO: users
		if mode == "solo" {
			type U struct {
				ID       int    `json:"id"`
				Username string `json:"username"`
				Points   int    `json:"points"`
			}

			qUsers := sq.Select("u.id", "u.username", "u.points").
				From("match_participants mp").
				Join("users u ON u.id = mp.user_id").
				Where(sq.Eq{"mp.match_id": matchID}).
				OrderBy("u.username ASC").
				PlaceholderFormat(sq.Dollar)

			rows, err := qQuery(ctx, db, qUsers)
			if err != nil {
				jsonErr(c, 500, "Ошибка сервера")
				return
			}
			defer rows.Close()

			users := []U{}
			for rows.Next() {
				var u U
				_ = rows.Scan(&u.ID, &u.Username, &u.Points)
				users = append(users, u)
			}

			out["users"] = users
			c.JSON(200, out)
			return
		}

		// 3) TEAM: teams with members
		type U struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
			Points   int    `json:"points"`
		}
		type T struct {
			ID      int    `json:"id"`
			Name    string `json:"name"`
			Members []U    `json:"members"`
		}

		qTeamRows := sq.Select("t.id", "t.name", "u.id", "u.username", "u.points").
			From("match_participants mp").
			Join("teams t ON t.id = mp.team_id").
			Join("users u ON u.id = mp.user_id").
			Where(sq.Eq{"mp.match_id": matchID}).
			Where(sq.Expr("mp.team_id IS NOT NULL")).
			OrderBy("t.name ASC", "u.username ASC").
			PlaceholderFormat(sq.Dollar)

		rows, err := qQuery(ctx, db, qTeamRows)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		defer rows.Close()

		teamMap := map[int]*T{}
		order := []int{}

		for rows.Next() {
			var tid int
			var tname string
			var u U
			_ = rows.Scan(&tid, &tname, &u.ID, &u.Username, &u.Points)

			t, ok := teamMap[tid]
			if !ok {
				t = &T{ID: tid, Name: tname, Members: []U{}}
				teamMap[tid] = t
				order = append(order, tid)
			}
			t.Members = append(t.Members, u)
		}

		teams := []T{}
		for _, tid := range order {
			teams = append(teams, *teamMap[tid])
		}

		out["teams"] = teams
		c.JSON(200, out)
	}
}

/* ===================== ADMIN: SET WINNER ===================== */

func AdminSetWinner(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor := uid(c)
		matchID, _ := strconv.Atoi(c.Param("id"))
		if matchID <= 0 {
			jsonErr(c, 400, "Некорректный матч")
			return
		}

		var req struct {
			WinnerUserID *int `json:"winner_user_id"`
			WinnerTeamID *int `json:"winner_team_id"`
			BonusPoints  int  `json:"bonus_points"`
		}
		if err := c.BindJSON(&req); err != nil {
			jsonErr(c, 400, "Некорректные данные")
			return
		}

		if (req.WinnerUserID == nil && req.WinnerTeamID == nil) ||
			(req.WinnerUserID != nil && req.WinnerTeamID != nil) {
			jsonErr(c, 400, "Выберите победителя")
			return
		}

		if req.BonusPoints < 0 || req.BonusPoints > 1_000_000 {
			jsonErr(c, 400, "Некорректные очки")
			return
		}

		ctx := context.Background()
		tx, err := db.Begin(ctx)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		defer tx.Rollback(ctx)

		// Проверка матча
		var mStatus, mTitle string
		qM := sq.Select("status", "title").
			From("matches").
			Where(sq.Eq{"id": matchID}).
			PlaceholderFormat(sq.Dollar)

		if err := qRowTx(ctx, tx, qM).Scan(&mStatus, &mTitle); err != nil {
			jsonErr(c, 404, "Матч не найден")
			return
		}
		if mStatus == "finished" {
			jsonErr(c, 400, "Матч уже завершён")
			return
		}

		// Проверка участия
		if req.WinnerUserID != nil {
			sub := sq.Select("1").
				From("match_participants").
				Where(sq.Eq{
					"match_id": matchID,
					"user_id":  *req.WinnerUserID,
				}).
				PlaceholderFormat(sq.Dollar)

			qExists := sq.Select().
				Column(sq.Expr("EXISTS(?)", sub)).
				PlaceholderFormat(sq.Dollar)

			var ok bool
			_ = qRowTx(ctx, tx, qExists).Scan(&ok)
			if !ok {
				jsonErr(c, 400, "Победитель не участвует в матче")
				return
			}
		} else {
			sub := sq.Select("1").
				From("match_participants").
				Where(sq.Eq{
					"match_id": matchID,
					"team_id":  *req.WinnerTeamID,
				}).
				PlaceholderFormat(sq.Dollar)

			qExists := sq.Select().
				Column(sq.Expr("EXISTS(?)", sub)).
				PlaceholderFormat(sq.Dollar)

			var ok bool
			_ = qRowTx(ctx, tx, qExists).Scan(&ok)
			if !ok {
				jsonErr(c, 400, "Победитель не участвует в матче")
				return
			}
		}

		// Завершаем матч
		updM := sq.Update("matches").
			Set("status", "finished").
			Set("winner_user_id", req.WinnerUserID).
			Set("winner_team_id", req.WinnerTeamID).
			Where(sq.Eq{"id": matchID}).
			PlaceholderFormat(sq.Dollar)

		if _, err := qExecTx(ctx, tx, updM); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		// Начисление очков
		if req.BonusPoints > 0 {

			// SOLO
			if req.WinnerUserID != nil {
				updU := sq.Update("users").
					Set("points", sq.Expr("points + ?", req.BonusPoints)).
					Where(sq.Eq{"id": *req.WinnerUserID}).
					PlaceholderFormat(sq.Dollar)

				if _, err := qExecTx(ctx, tx, updU); err != nil {
					jsonErr(c, 500, "Ошибка сервера")
					return
				}

			} else {

				// TEAM — получаем список участников
				qMembers := sq.Select("user_id").
					From("team_members").
					Where(sq.Eq{"team_id": *req.WinnerTeamID}).
					PlaceholderFormat(sq.Dollar)

				rows, err := qQueryTx(ctx, tx, qMembers)
				if err != nil {
					jsonErr(c, 500, "Ошибка сервера")
					return
				}
				defer rows.Close()

				ids := make([]int, 0, 16)
				for rows.Next() {
					var id int
					_ = rows.Scan(&id)
					if id > 0 {
						ids = append(ids, id)
					}
				}

				if len(ids) > 0 {
					updTeam := sq.Update("users").
						Set("points", sq.Expr("points + ?", req.BonusPoints)).
						Where(sq.Eq{"id": ids}).
						PlaceholderFormat(sq.Dollar)

					if _, err := qExecTx(ctx, tx, updTeam); err != nil {
						jsonErr(c, 500, "Ошибка сервера")
						return
					}
				}
			}
		}

		if err := tx.Commit(ctx); err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}

		logAction(db, &actor, "admin_set_winner",
			"Администратор завершил матч: "+clampRunes(mTitle, MaxReportLine))

		c.JSON(200, gin.H{"ok": true})
	}
}

/* ===================== ADMIN: REPORT ===================== */

func AdminMatchReport(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		matchID, _ := strconv.Atoi(c.Param("id"))
		if matchID <= 0 {
			jsonErr(c, 400, "Некорректный матч")
			return
		}

		ctx := context.Background()

		var title, mode, status string
		var winnerUserID *int
		var winnerTeamID *int

		qM := sq.Select("title", "mode", "status", "winner_user_id", "winner_team_id").
			From("matches").
			Where(sq.Eq{"id": matchID}).
			PlaceholderFormat(sq.Dollar)

		if err := qRow(ctx, db, qM).Scan(&title, &mode, &status, &winnerUserID, &winnerTeamID); err != nil {
			jsonErr(c, 404, "Матч не найден")
			return
		}

		// ✅ заявки: только агрегаты (без id/username)
		type appAgg struct {
			Status string
			Cnt    int
		}
		qApps := sq.Select("status", "COUNT(*)").
			From("applications").
			Where(sq.Eq{"match_id": matchID}).
			GroupBy("status").
			OrderBy("status").
			PlaceholderFormat(sq.Dollar)

		rowsA, err := qQuery(ctx, db, qApps)
		if err != nil {
			jsonErr(c, 500, "Ошибка сервера")
			return
		}
		defer rowsA.Close()

		apps := []appAgg{}
		for rowsA.Next() {
			var r appAgg
			_ = rowsA.Scan(&r.Status, &r.Cnt)
			apps = append(apps, r)
		}

		// ✅ участники: только количество
		qCnt := sq.Select("COUNT(*)").
			From("match_participants").
			Where(sq.Eq{"match_id": matchID}).
			PlaceholderFormat(sq.Dollar)

		var participants int
		_ = qRow(ctx, db, qCnt).Scan(&participants)

		// ✅ победитель: без id/username (команде можно показать название)
		winnerStr := "не определён"
		if winnerTeamID != nil {
			var tn string
			qT := sq.Select("name").
				From("teams").
				Where(sq.Eq{"id": *winnerTeamID}).
				PlaceholderFormat(sq.Dollar)
			_ = qRow(ctx, db, qT).Scan(&tn)
			if strings.TrimSpace(tn) != "" {
				winnerStr = "Команда: " + clampRunes(tn, MaxReportLine)
			} else {
				winnerStr = "Команда"
			}
		} else if winnerUserID != nil {
			winnerStr = "Пользователь"
		}

		// отчёт
		title = clampRunes(title, MaxReportLine)
		report := ""
		report += "ОТЧЁТ ПО МАТЧУ\n"
		report += "Название: " + title + "\n"
		report += "Режим: " + mode + "\n"
		report += "Статус: " + status + "\n"
		report += "Победитель: " + winnerStr + "\n"
		report += "Участников: " + strconv.Itoa(participants) + "\n\n"

		report += "Заявки:\n"
		if len(apps) == 0 {
			report += "- нет\n"
		} else {
			for _, a := range apps {
				report += "- " + a.Status + ": " + strconv.Itoa(a.Cnt) + "\n"
			}
		}

		c.JSON(200, gin.H{"report": report})
	}
}
