package main

import (
	"database/sql"
	"net/url"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/asaskevich/govalidator"
	"github.com/gin-gonic/gin"
	"zxq.co/ripple/schiavolib"
)

func register(c *gin.Context) {
	if getContext(c).User.ID != 0 {
		resp403(c)
		return
	}

	if c.Query("stopsign") != "1" {
		u, _ := tryBotnets(c)
		if u != "" {
			simple(c, getSimpleByFilename("register/elmo.html"), nil, map[string]interface{}{
				"Username": u,
			})
			return
		}
	}

	registerResp(c)
}

func registerSubmit(c *gin.Context) {
	if getContext(c).User.ID != 0 {
		resp403(c)
		return
	}

	// check username is valid by our criteria
	username := strings.TrimSpace(c.PostForm("username"))
	if !usernameRegex.MatchString(username) {
		registerResp(c, errorMessage{T(c, "Your username must contain alphanumerical characters, spaces, or any of <code>_[]-</code>")})
		return
	}

	/* beta keys
	key := strings.TrimSpace(c.PostForm("key"))
	if db.QueryRow("SELECT 1 FROM beta_keys WHERE key = ?", c.PostForm("key")).
		Scan(new(int)) ==  sql.ErrNoRows {
		registerResp(c, errorMessage{T(c, "Your key is invalid!")})
		return
	}
	*/

	// check whether an username is in forbidden usernames.
	if in(strings.ToLower(username), forbiddenUsernames) {
		registerResp(c, errorMessage{T(c, "You're not allowed to register with that username.")})
		return
	}

	// check email is valid
	if !govalidator.IsEmail(c.PostForm("email")) {
		registerResp(c, errorMessage{T(c, "Please pass a valid email address.")})
		return
	}

	// passwords check (too short/too common)
	if x := validatePassword(c.PostForm("password")); x != "" {
		registerResp(c, errorMessage{T(c, x)})
		return
	}

	// usernames with both _ and spaces are not allowed
	if strings.Contains(username, "_") && strings.Contains(username, " ") {
		registerResp(c, errorMessage{T(c, "An username can't contain both underscores and spaces.")})
		return
	}

	// check whether username already exists
	if db.QueryRow("SELECT 1 FROM users WHERE safe_name = ?", safeUsername(username)).
		Scan(new(int)) != sql.ErrNoRows {
		registerResp(c, errorMessage{T(c, "An user with that username already exists!")})
		return
	}

	// check whether an user with that email already exists
	if db.QueryRow("SELECT 1 FROM users WHERE email = ?", c.PostForm("email")).
		Scan(new(int)) != sql.ErrNoRows {
		registerResp(c, errorMessage{T(c, "An user with that email address already exists!")})
		return
	}

	// recaptcha verify
	if config.RecaptchaPrivate != "" && !recaptchaCheck(c) {
		registerResp(c, errorMessage{T(c, "Captcha is invalid.")})
		return
	}

	uMulti, criteria := tryBotnets(c)
	if criteria != "" {
		schiavo.CMs.Send(
			fmt.Sprintf(
				"User **%s** registered with the same %s as %s (%s/u/%s). **POSSIBLE MULTIACCOUNT!!!**. Waiting for ingame verification...",
				username, criteria, uMulti, config.BaseURL, url.QueryEscape(uMulti),
			),
		)
	}

	// The actual registration.
	pass, err := generatePassword(c.PostForm("password"))
	if err != nil {
		resp500(c)
		return
	}

	if len(c.Request.Header.Get("CF-IPCountry")) > 2 {
		registerResp(c, errorMessage{T(c, "Cloudflare error.")})
		return
	}

	res, err := db.Exec(`INSERT INTO users(name, safe_name, pw_bcrypt, email, creation_time, priv, latest_activity) VALUES (?, ?, ?, ?, ?, ?, ?);`,
		username, safeUsername(username), pass, c.PostForm("email"), time.Now().Unix(), NORMAL, time.Now().Unix())
	if err != nil {
		registerResp(c, errorMessage{T(c, "Whoops, an error slipped in. You might have been registered, though. I don't know.")})
		return
	}
	lid, _ := res.LastInsertId()

	for i := range []int{0,1,2,3,4,5,6,7} {
		db.Exec("INSERT INTO `stats` (id, mode) VALUES (?, ?)", lid, i)
	}

	/* Beta Keys
	db.Exec("UPDATE `beta_keys` set used = 1 where key = ?", key)

	// Ripple Gay Bot
	schiavo.CMs.Send(fmt.Sprintf("User (**%s** | %s) registered from %s", username, c.PostForm("email"), clientIP(c)))
	*/
	// delete the key c
	//db.Exec("DELETE FROM beta_keys WHERE beta_key = ?", c.PostForm("key"))

	setYCookie(int(lid), c)

	rd.Incr("ripple:registered_users")

	//addMessage(c, successMessage{T(c, "You have been successfully registered on Akatsuki!")})
	getSession(c).Save()
	c.Redirect(302, "/register/verify?u="+strconv.Itoa(int(lid)))
}

func registerResp(c *gin.Context, messages ...message) {
	resp(c, 200, "register/register.html", &baseTemplateData{
		TitleBar:  "Register",
		KyutGrill: "register.jpg",
		Scripts:   []string{"https://www.google.com/recaptcha/api.js"},
		Messages:  messages,
		FormData:  normaliseURLValues(c.Request.PostForm),
	})
}

func tryBotnets(c *gin.Context) (string, string) {
	var username string

	err := db.QueryRow("SELECT u.username FROM ip_user i INNER JOIN users u ON u.id = i.userid WHERE i.ip = ?", clientIP(c)).Scan(&username)
	if err != nil {
		if err != sql.ErrNoRows {
			c.Error(err)
		}
		return "", ""
	}
	if username != "" {
		return username, "IP"
	}

	cook, _ := c.Cookie("y")
	err = db.QueryRow("SELECT u.username FROM identity_tokens i INNER JOIN users u ON u.id = i.userid WHERE i.token = ?",
		cook).Scan(&username)
	if err != nil {
		if err != sql.ErrNoRows {
			c.Error(err)
		}
		return "", ""
	}
	if username != "" {
		return username, "username"
	}

	return "", ""
}

func verifyAccount(c *gin.Context) {
	if getContext(c).User.ID != 0 {
		resp403(c)
		return
	}

	/*
	i, ret := checkUInQS(c)
	if ret {
		return
	}

	sess := getSession(c)
	var rPrivileges uint64
	db.Get(&rPrivileges, "SELECT privileges FROM users WHERE id = ?", i)
	if common.UserPrivileges(rPrivileges)&common.UserPrivilegePendingVerification == 0 {
		//addMessage(c, warningMessage{T(c, "Nope.")})
		sess.Save()
		//c.Redirect(302, "/")
		//return
	}*/

	addMessage(c, successMessage{T(c, "You have been successfully registered on Akatsuki!")})

	resp(c, 200, "register/verify.html", &baseTemplateData{
		TitleBar:       "Welcome to Akatsuki!",
		HeadingOnRight: true,
		KyutGrill:      "welcome.jpg",
	})
}

func welcome(c *gin.Context) {
	if getContext(c).User.ID != 0 {
		resp403(c)
		return
	}

	i, ret := checkUInQS(c)
	if ret {
		return
	}

	var rPrivileges uint64
	db.Get(&rPrivileges, "SELECT priv FROM users WHERE id = ?", i)
	if Privileges(rPrivileges) & VERIFIED == 0 {
		c.Redirect(302, "/register/verify?u="+c.Query("u"))
		return
	}

	t := T(c, "Welcome!")
	if Privileges(rPrivileges) & NORMAL == 0 {
		// if the user has no UserNormal, it means they're banned = they multiaccounted
		t = T(c, "Welcome back!")
	}

	resp(c, 200, "register/welcome.html", &baseTemplateData{
		TitleBar:       t,
		HeadingOnRight: true,
		KyutGrill:      "welcome.jpg",
	})
}

// Check User In Query Is Same As User In Y Cookie
func checkUInQS(c *gin.Context) (int, bool) {
	sess := getSession(c)

	i, _ := strconv.Atoi(c.Query("u"))
	y, _ := c.Cookie("y")
	err := db.QueryRow("SELECT 1 FROM identity_tokens WHERE token = ? AND userid = ?", y, i).Scan(new(int))
	if err == sql.ErrNoRows {
		addMessage(c, warningMessage{T(c, "Nope.")})
		sess.Save()
		c.Redirect(302, "/")
		return 0, true
	}
	return i, false
}

func in(s string, ss []string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

var usernameRegex = regexp.MustCompile(`^[A-Za-z0-9 _\[\]-]{2,15}$`)
var forbiddenUsernames = []string{
    "rrtyui", "cookiezi", "azer", "happystick", "doomsday", "sharingan33", "andrea", "cptnxn", "reimu-desu", "hvick225", "_index",
    "my aim sucks", "kynan", "rafis", "sayonara-bye", "thelewa", "wubwoofwolf", "millhioref", "tom94", "clsw", "spectator", "exgon",
	"axarious", "angelsim", "recia", "nara", "emperorpenguin83", "bikko", "xilver", "vettel", "kuu01", "_yu68", "tasuke912",
	"dusk", "ttobas", "velperk", "jakads", "jhlee0133", "abcdullah", "yuko-", "entozer", "hdhr", "ekoro", "snowwhite", "osuplayer111",
	"musty", "nero", "elysion", "ztrot", "koreapenguin", "fort", "asphyxia", "niko", "shigetora", "kaoru", "Smoothieworld", "toy", "[toy]",
	"ozzyozrock", "fieryrage", "gosy777", "zyph", "beasttrollmc", "adamqs", "karthy", "fenrir", "rohulk", "_ryuk", "spajder", "fartownik",
	"cxu", "dunois", "ner0", "wiltchq", "-gn", "cinia pacifica", "yaong", "zeluar", "dsan", "dustice", "rucker", "firebat92", "avenging_goose",
	"idke", "vaxei", "seouless", "spare", "totoki", "rustbell", "emilia", "reimu-desu", "tiger claw", "boggles", "thepoon", "the poon", "loli_silica",
	"bahamete", "bikko", "la valse", "thelewa", "firstus", "ritzeh", "kablaze", "peppy", "loctav", "banchobot", "millhioref", "ephemeral", "flyte",
	"nanaya", "RBRat3", "smoogipoooo", "tom94", "yelle", "ztrot", "zallius", "deadbeat", "shaRPLL", "shaRPII", "shARPIL", "shARPLI", "Blaizer",
	"Damnae", "Daru", "Echo", "fly a kite", "marcin", "mm201", "nekodex", "rbrat3", "thevileone", "alumentorz", "fort", "11t", "captin1", "kroytz",
	"cryo[iceeicee]", "Akali", "professionalbox", "Fantazy", "Sing", "toybot", "goldenwolf", "handsome", "Raikozen", "cherry blossom",
	"monstrata", "Ascendence", "doorfin", "barkingmaddog", "Karen", "crystal", "vert", "halfslashed", "kloyd", "djpop", "cyclone", "guy", "sakura",
	"spectator", "pishifat", "ktgster", "skystar", "o9kami", "09kami", "Nathan", "ely", "hollow wings", "val0108", "blue dragon", "tillerino",
	"mikuia", "ameo", "tatsumaki", "cmyui", "solis", "rumoi", "frostidrinks", "cursordance", "parkes", "paparkes", "daniel", "flyingtuna",
	"walkingtuna", "nathan on osu", "justice", "child", "eb", "kalzo", "ebenezer", "solomon", "murmurtwins", "ggm9", "kaguya", "unspoken mattay",
	"mattay", "parkourwizard", "woey", "trafis", "klug", "c o i n", "varvalian", "mismagius", "nameless player", "mbmasher", "okinamo", "knalli",
	"obtio", "konnan", "ppy", "nejzha", "kochiya", "haruki", "kaguya", "miniature lamp", "phabled", "hentai", "coletaku", "zoom", "mathyu",
	"windshear", "roma4ka", "bad girl", "arfung", "skyapple", "hotzi6", "joueur de visee", "ted", "willcookie", "zerrah", "-ristuki", "yuudachi",
	"idealism", "shiiiiiii", "shayell", "parky", "torahiko", "digidrake", "a12456", "chal", "mathi", "relaxingtuna", "eriksu", "firedigger", "-hibiki-",
	"notititititi", "mysliderbreak", "qsc20010", "curry3521", "s1ck", "itswinter", "remillia", "astar", "aika", "ruri", "cpugeek", "andros",
	"xeltol", "merami", "mrekk", "whitecat", "micca", "alumetri", "fgsky", "badeu", "asecretbox", "a_blue", "lifeline", "dereban", "vamhi",
	"azr8", "azerite", "ralidea", "bartek22830", "morgn", "maxim bogdan", "gasha", "chocomint", "srchispa", "vinno", "mcy4", "arcin", "gayzmcgee",
	"filsdelama", "paraqeet", "danyl",
}
