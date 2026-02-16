package main

import (
	"log"
	"os"

	"ctf-platform/internal"

	"github.com/gin-gonic/gin"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("JWT_SECRET is required")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	db := internal.MustDB(dbURL)
	defer db.Close()

	r := gin.Default()

	// Frontend static
	r.Static("/static", "/app/static")
	r.GET("/", func(c *gin.Context) { c.File("/app/static/index.html") })
	r.GET("/login", func(c *gin.Context) { c.File("/app/static/login.html") })
	r.GET("/register", func(c *gin.Context) { c.File("/app/static/register.html") })
	r.GET("/dashboard", func(c *gin.Context) { c.File("/app/static/dashboard.html") })

	api := r.Group("/api")
	{
		api.POST("/auth/register", internal.Register(db))
		api.POST("/auth/login", internal.Login(db, secret))
		api.POST("/auth/logout", internal.Logout())
		api.GET("/me", internal.Auth(secret), internal.Me(db))

		api.GET("/rating", internal.Auth(secret), internal.Rating(db))

		// matches/applications/history (status: open|finished|all)
		api.GET("/matches", internal.Auth(secret), internal.ListMatches(db))
		api.POST("/matches/:id/apply", internal.Auth(secret), internal.ApplyToMatch(db))
		api.GET("/my/applications", internal.Auth(secret), internal.MyApplications(db))
		api.GET("/history", internal.Auth(secret), internal.MyHistory(db))

		// teams
		api.POST("/teams", internal.Auth(secret), internal.CreateTeam(db))
		api.GET("/teams/open", internal.Auth(secret), internal.ListOpenTeams(db))
		api.GET("/my/teams", internal.Auth(secret), internal.MyTeams(db))
		api.POST("/teams/:id/join", internal.Auth(secret), internal.JoinTeam(db))
		api.POST("/teams/:id/leave", internal.Auth(secret), internal.LeaveTeam(db))

		// admin
		admin := api.Group("/admin", internal.Auth(secret), internal.RequireAdmin())
		{
			admin.GET("/logs", internal.AdminLogs(db))

			admin.GET("/users", internal.AdminUsers(db))
			admin.DELETE("/users/:id", internal.AdminDeleteUser(db))
			admin.POST("/users/:id/points", internal.AdminSetPoints(db))

			admin.POST("/matches", internal.AdminCreateMatch(db))
			admin.PUT("/matches/:id", internal.AdminUpdateMatch(db))
			admin.DELETE("/matches/:id", internal.AdminDeleteMatch(db))
			admin.GET("/matches", internal.AdminListMatches(db)) // ?status=open|finished|all
			admin.GET("/matches/:id/participants", internal.AdminMatchParticipants(db)) // only for open
			admin.POST("/matches/:id/winner", internal.AdminSetWinner(db)) // finish match
			admin.GET("/matches/:id/report", internal.AdminMatchReport(db))

			admin.GET("/applications", internal.AdminListApplications(db))
			admin.POST("/applications/:id/approve", internal.AdminApproveApplication(db))
			admin.POST("/applications/:id/reject", internal.AdminRejectApplication(db))

			// teams admin
			admin.GET("/teams", internal.AdminListTeams(db))
			admin.POST("/teams/:id/add-user", internal.AdminAddUserToTeam(db))
		}
	}

	log.Printf("Listening on :%s", port)
	_ = r.Run(":" + port)
}
