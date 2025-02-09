package main

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"zxq.co/x/rs"
)

func ccreate(c *gin.Context) {
	ccreateResp(c)
}

func ccreateSubmit(c *gin.Context) {
	if getContext(c).User.ID == 0 {
		resp403(c)
		return
	}
	// check registrations are enabled
	if !ccreationEnabled() {
		ccreateResp(c, errorMessage{T(c, "Sorry, it's not possible to create a clan at the moment. Please try again later.")})
		return
	}

	// check username is valid by our criteria
	username := strings.TrimSpace(c.PostForm("username"))
	if !cnameRegex.MatchString(username) {
		ccreateResp(c, errorMessage{T(c, "Your clans name must contain alphanumerical characters, spaces, or any of <code>_[]-</code>.")})
		return
	}

	// check whether name already exists
	if db.QueryRow("SELECT 1 FROM clans WHERE name = ?", c.PostForm("username")).
		Scan(new(int)) != sql.ErrNoRows {
		ccreateResp(c, errorMessage{T(c, "A clan with that name already exists!")})
		return
	}

	// check whether tag already exists
	if db.QueryRow("SELECT 1 FROM clans WHERE tag = ?", c.PostForm("tag")).
		Scan(new(int)) != sql.ErrNoRows {
		ccreateResp(c, errorMessage{T(c, "A clan with that tag already exists!")})
		return
	}

	// recaptcha verify

	tag := "0"
	if c.PostForm("tag") != "" {
		tag = c.PostForm("tag")
	}

	// The actual registration.

	invite := rs.String(8)

	for db.QueryRow("SELECT 1 FROM clans WHERE invite = ?", invite).Scan(new(int)) != sql.ErrNoRows {
		invite = rs.String(8)
	}

	res, err := db.Exec(`INSERT INTO clans(name, description, icon, tag, owner, invite, status)
							  VALUES (?, ?, ?, ?, ?, ?, 2);`,
		username, c.PostForm("password"), c.PostForm("email"), tag, getContext(c).User.ID, invite)
	if err != nil {
		ccreateResp(c, errorMessage{T(c, "Whoops, an error slipped in. Clan might have been created, though. I don't know.")})
		fmt.Println(err)
		return
	}
	lid, _ := res.LastInsertId()

	db.Exec("UPDATE users SET clan_id = ?, clan_privileges = 8 WHERE id = ?", lid, getContext(c).User.ID)

	addMessage(c, successMessage{T(c, "Clan created.")})
	getSession(c).Save()
	c.Redirect(302, "/c/"+strconv.Itoa(int(lid)))
}

func ccreateResp(c *gin.Context, messages ...message) {
	resp(c, 200, "clans/create.html", &baseTemplateData{
		TitleBar:  "Create Clan",
		KyutGrill: "register.jpg",
		Messages:  messages,
		FormData:  normaliseURLValues(c.Request.PostForm),
	})
}

func ccreationEnabled() bool {
	var enabled bool
	db.QueryRow("SELECT value_int FROM system_settings WHERE name = 'ccreation_enabled'").Scan(&enabled)
	return enabled
}

var cnameRegex = regexp.MustCompile(`^[A-Za-z0-9 '_\[\]-]{2,15}$`)
