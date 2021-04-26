package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
)

var conn *pgx.Conn

type PostAtTime struct {
	Year  int32
	Image string
	Movie string
}

type MyPost struct {
	MediaUrl  string `json:"media_url"`
	Caption   string `json:"caption"`
	Timestamp string `json:"timestamp"`
	Id        string `json:"id"`
}
type CursorInfo struct {
	Before string `json:"before"`
	After  string `json:"after"`
}
type PagingInfo struct {
	Cursor CursorInfo `json:"cursors"`
	Next   string     `json:"next"`
}
type MyHistory struct {
	Data   []MyPost   `json:"data"`
	Paging PagingInfo `json:"paging"`
}

func main() {
	var err error
	conn, err = pgx.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connection to database: %v\n", err)
		os.Exit(1)
	}

	var posts []PostAtTime

	r := gin.Default()
	r.LoadHTMLGlob("templates/*")
	r.GET("/index", func(c *gin.Context) {
		posts = []PostAtTime{}
		loadPosts(&posts)
		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"posts": posts,
		})
	})
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "pong"})
	})
	r.GET("/all", func(c *gin.Context) {
		ret := insertAllPosts()
		var val string
		if ret < 0 {
			val = "error occurred"
		} else if ret == 0 {
			val = "no new posts"
		} else {
			val = fmt.Sprintf("%d posts inserted.", ret)
		}
		c.JSON(200, gin.H{"result": val})
	})
	r.GET("/update", func(c *gin.Context) {
		ret := checkNewPosts()
		var val string
		if ret < 0 {
			val = "error occurred"
		} else if ret == 0 {
			val = "no new posts"
		} else {
			val = fmt.Sprintf("%d posts inserted.", ret)
		}
		c.JSON(200, gin.H{"result": val})
	})
	r.Run()
}

func checkNewPosts() int {
	token := os.Getenv("API_TOKEN")
	url := fmt.Sprintf("https://graph.instagram.com/me/media?fields=media_url,caption,timestamp&access_token=%s", token)
	res, err1 := http.Get(url)
	if err1 != nil {
		fmt.Fprintf(os.Stderr, "API失敗")
		return -1
	}
	defer res.Body.Close()
	byteArray, err2 := ioutil.ReadAll(res.Body)
	if err2 != nil {
		fmt.Fprintf(os.Stderr, "レスポンス読み込み失敗")
		return -1
	}

	var h MyHistory
	err3 := json.Unmarshal(byteArray, &h)
	if err3 != nil {
		fmt.Fprintf(os.Stderr, "json変換失敗")
		return -1
	}
	return addPost(h)
}

func insertAllPosts() int {
	cnt := 0
	token := os.Getenv("API_TOKEN")
	url := fmt.Sprintf("https://graph.instagram.com/me/media?fields=media_url,caption,timestamp&access_token=%s", token)
	for {
		res, err1 := http.Get(url)
		if err1 != nil {
			fmt.Fprintf(os.Stderr, "API失敗")
			return cnt
		}
		defer res.Body.Close()
		byteArray, err2 := ioutil.ReadAll(res.Body)
		if err2 != nil {
			fmt.Fprintf(os.Stderr, "レスポンス読み込み失敗")
			return cnt
		}

		var h MyHistory
		err3 := json.Unmarshal(byteArray, &h)
		if err3 != nil {
			fmt.Fprintf(os.Stderr, "json変換失敗")
			return cnt
		}
		addPost(h)

		url = h.Paging.Next
		if h.Paging.Next == "" {
			break
		}
	}
	return cnt
}

func addPost(history MyHistory) int {
	cnt := 0
	for _, p := range history.Data {
		t, e := time.Parse("2006-01-02T15:04:05+0000", p.Timestamp)
		if e != nil {
			fmt.Fprintf(os.Stderr, "time parse error: %v\n", e)
			return cnt
		}

		rows, _ := conn.Query(context.Background(), "select * from posts where post_at = $1", p.Timestamp)
		if rows.Next() {
			// 登録済みなので戻す
			return cnt
		}

		_, err := conn.Exec(context.Background(), "insert into posts(post_at, url, caption, year, month, day) values($1, $2, $3, $4, $5, $6)", p.Timestamp, p.MediaUrl, p.Caption, t.Year(), int(t.Month()), t.Day())
		if err != nil {
			fmt.Fprintf(os.Stderr, "insert failed: %v\n", err)
			return cnt
		}
		cnt++
	}
	return cnt
}

func loadPosts(posts *[]PostAtTime) {
	var err error

	dt := time.Now()
	rows, _ := conn.Query(context.Background(), "select url, year from posts where month = $1 and day = $2 order by year desc", int(dt.Month()), dt.Day())

	for rows.Next() {
		var url string
		var year int32

		err = rows.Scan(&url, &year)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable scan: %v\n", err)
			return
		}
		fmt.Println("$1", year)
		if strings.Contains(url, "https://video") {
			post := PostAtTime{year, "", url}
			*posts = append(*posts, post)
		} else {
			post := PostAtTime{year, url, ""}
			*posts = append(*posts, post)
		}
	}
}
