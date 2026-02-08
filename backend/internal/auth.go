package internal

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func Register(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Username  string `json:"username"`
			Password  string `json:"password"`
			Password2 string `json:"password2"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "bad json"})
			return
		}
		if req.Username == "" || req.Password == "" || req.Password2 == "" {
			c.JSON(400, gin.H{"error": "fill all fields"})
			return
		}
		if req.Password != req.Password2 {
			c.JSON(400, gin.H{"error": "passwords do not match"})
			return
		}
		if len(req.Password) < 6 {
			c.JSON(400, gin.H{"error": "password too short"})
			return
		}

		hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), 10)

		var id int
		err := db.QueryRow(context.Background(),
			"INSERT INTO users(username, pass_hash, role) VALUES ($1,$2,'user') RETURNING id",
			req.Username, string(hash),
		).Scan(&id)
		if err != nil {
			c.JSON(409, gin.H{"error": "username already exists"})
			return
		}
		logAction(db, &id, "register", "user registered")
		c.JSON(200, gin.H{"ok": true})
	}
}

func Login(db *pgxpool.Pool, secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "bad json"})
			return
		}

		var u User
		var passHash string
		err := db.QueryRow(context.Background(),
			"SELECT id, username, role, points, pass_hash FROM users WHERE username=$1",
			req.Username,
		).Scan(&u.ID, &u.Username, &u.Role, &u.Points, &passHash)
		if err != nil {
			c.JSON(401, gin.H{"error": "invalid credentials"})
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(passHash), []byte(req.Password)) != nil {
			c.JSON(401, gin.H{"error": "invalid credentials"})
			return
		}

		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
			UserID: u.ID,
			Role:   u.Role,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				Issuer:    "ctf-platform",
			},
		})
		s, _ := tok.SignedString([]byte(secret))

		secure := os.Getenv("COOKIE_SECURE") == "1"
		c.SetCookie(cookieName, s, 86400, "/", "", secure, true)

		logAction(db, &u.ID, "login", "success")
		c.JSON(200, gin.H{"ok": true})
	}
}

func Logout() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.SetCookie(cookieName, "", -1, "/", "", false, true)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
