package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/dustin/go-humanize"
	"github.com/fabioberger/airtable-go"
	"github.com/getsentry/raven-go"
	"github.com/jmcvetta/randutil"
	"github.com/knq/baseconv"
	"github.com/mattn/go-mastodon"
	"github.com/mmcdole/gofeed"
	"github.com/vincent-petithory/dataurl"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/ioutil"
	"log"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Bot struct {
	ID     string
	Fields struct {
		Name                       string
		Enabled                    bool
		Registered                 bool
		Activated                  bool
		BotInstance                string
		BotUsername                string
		BotEmail                   string
		BotName                    string
		BotBio                     string
		RSSUrl                     string
		RSSTemplate                string
		LastPostedAt               string
		BotApplicationName         string
		BotApplicationUrl          string
		BotApplicationId           string
		BotApplicationClientId     string
		BotApplicationClientSecret string
		BotPassword                string
		LastCheckedAt              string
		BotAvatarUrl               string
		BotAvatarUploaded          bool
		RSSLastGUIDs               string
		CleanAndReset              bool
	}
}

func main() {
	ravenDSN := os.Getenv("RAVEN_DSN")
	raven.SetDSN(ravenDSN)

	airtableAPIKey := os.Getenv("AIRTABLE_API_KEY")
	baseID := os.Getenv("AIRTABLE_BASE_ID")

	client, err := airtable.New(airtableAPIKey, baseID)
	if err != nil {
		panic(err)
	}

	bots := []Bot{}
	if err := client.ListRecords("Bot List", &bots); err != nil {
		panic(err)
	}

	for _, bot := range bots {
		result := bot.Process()
		if len(result) > 0 {
			if err := client.UpdateRecord("Bot List", bot.ID, result, &bot); err != nil {
				panic(err)
			}
		}
	}

	fmt.Printf("Hello, world.\n")
}

func ReportError(err error) {
	raven.CaptureErrorAndWait(err, nil)
	log.Print(err)
	debug.PrintStack()
}

func (bot *Bot) Process() map[string]interface{} {
	ctx := context.Background()
	updatedFields := map[string]interface{}{}
	if !bot.Fields.Enabled {
		return updatedFields
	}

	fmt.Printf("Bot Status Fetched: %#v\n", bot)

	if bot.Fields.BotApplicationClientId == "" {
		app, err := mastodon.RegisterApp(ctx, &mastodon.AppConfig{
			Server:     "https://" + bot.Fields.BotInstance,
			ClientName: bot.Fields.BotApplicationName,
			Scopes:     "read write follow",
			Website:    bot.Fields.BotApplicationUrl,
		})
		if err != nil {
			panic(err)
		}

		bot.Fields.BotApplicationId = strconv.FormatInt(app.ID, 10)
		bot.Fields.BotApplicationClientId = app.ClientID
		bot.Fields.BotApplicationClientSecret = app.ClientSecret

		updatedFields["BotApplicationId"] = bot.Fields.BotApplicationId
		updatedFields["BotApplicationClientId"] = bot.Fields.BotApplicationClientId
		updatedFields["BotApplicationClientSecret"] = bot.Fields.BotApplicationClientSecret
	}

	if bot.Fields.BotPassword == "" {
		password, err := randutil.AlphaString(48)
		if err != nil {
			panic(err)
		}
		fmt.Printf("Generated password for %s@%s is %s\n", bot.Fields.BotUsername, bot.Fields.BotInstance, password)

		bot.Fields.BotPassword = password
		updatedFields["BotPassword"] = bot.Fields.BotPassword
	}

	if !bot.Fields.Registered {
		jar, err := cookiejar.New(nil)
		if err != nil {
			panic(err)
		}

		token := GetMastodonAuthenticityToken(bot.Fields.BotInstance, "/auth/sign_up", jar)
		RegisterMastodonUser(bot.Fields.BotInstance, token, bot.Fields.BotUsername, bot.Fields.BotPassword, bot.Fields.BotEmail, jar)

		bot.Fields.Registered = true
		updatedFields["Registered"] = bot.Fields.Registered
	}

	if bot.Fields.Activated {
		c := mastodon.NewClient(&mastodon.Config{
			Server:       "https://" + bot.Fields.BotInstance,
			ClientID:     bot.Fields.BotApplicationClientId,
			ClientSecret: bot.Fields.BotApplicationClientSecret,
		})

		err := c.Authenticate(ctx, bot.Fields.BotEmail, bot.Fields.BotPassword)
		if err != nil {
			panic(err)
		}

		account, err := c.GetAccountCurrentUser(ctx)
		if err != nil {
			panic(err)
		}

		newProfile := mastodon.Profile{}
		shouldUpdateProfile := false

		if bot.Fields.BotAvatarUrl != "" && !bot.Fields.BotAvatarUploaded {
			shouldUpdateProfile = true

			resp, err := http.Get(bot.Fields.BotAvatarUrl)
			if err != nil {
				panic(err)
			}
			defer resp.Body.Close()

			picture, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				panic(err)
			}

			newProfile.Avatar = dataurl.EncodeBytes(picture)

			bot.Fields.BotAvatarUploaded = true
			updatedFields["BotAvatarUploaded"] = bot.Fields.BotAvatarUploaded
		}

		if account.DisplayName != bot.Fields.BotName || account.Note != bot.Fields.BotBio {
			newProfile.DisplayName = &bot.Fields.BotName
			newProfile.Note = &bot.Fields.BotBio

			shouldUpdateProfile = true
		}

		if shouldUpdateProfile {
			newAccount, err := c.AccountUpdate(ctx, &newProfile)
			if err != nil {
				panic(err)
			}

			account = newAccount
		}

		if bot.Fields.CleanAndReset {
			for {
				status, err := c.GetAccountStatuses(ctx, account.ID)
				if err != nil {
					panic(err)
				}

				if len(status) == 0 {
					break
				}

				for _, item := range status {
					c.DeleteStatus(ctx, item.ID)
				}
			}

			bot.Fields.RSSLastGUIDs = ""
			bot.Fields.CleanAndReset = false
			updatedFields["RSSLastGUIDs"] = bot.Fields.RSSLastGUIDs
			updatedFields["CleanAndReset"] = bot.Fields.CleanAndReset
		}

		{
			resp, err := http.Get(Protocol(bot.Fields.RSSUrl))
			if err != nil {
				panic(err)
			}
			defer resp.Body.Close()

			fp := gofeed.NewParser()
			feed, err := fp.Parse(resp.Body)
			if err != nil {
				panic(err)
			}

			sort.Slice(feed.Items, func(i, j int) bool { return feed.Items[i].PublishedParsed.Before(*feed.Items[j].PublishedParsed) })

			lastGuids := strings.Split(bot.Fields.RSSLastGUIDs, "||||")

			for _, item := range feed.Items {
				fmt.Printf("Processing Item: %s\n", item.GUID)
				thisGuid, err := CreateGUID(bot.Fields.RSSUrl, item)
				if err != nil {
					ReportError(err)
					break
				}

				if stringInSlice(*thisGuid, lastGuids) {
					fmt.Printf("Duplicated Item: %s\n", *thisGuid)
					continue
				}
				fmt.Printf("New Item: %s\n", *thisGuid)

				toot, err := CreateToot(bot.Fields.RSSUrl, item, c, &ctx)
				if err != nil {
					ReportError(err)
					break
				}
				fmt.Printf("Generated Toot: %#v\n", toot)

				_, err = c.PostStatus(ctx, toot)
				if err != nil {
					ReportError(err)
					break
				}

				lastGuids = append(lastGuids, *thisGuid)
				bot.Fields.LastPostedAt = time.Now().UTC().Format(time.RFC3339)
				updatedFields["LastPostedAt"] = bot.Fields.LastPostedAt
			}

			maxGuidItems := len(feed.Items) * 10
			if maxGuidItems < 100 {
				maxGuidItems = 100
			}

			firstGuidItem := len(lastGuids) - maxGuidItems
			if firstGuidItem < 0 {
				firstGuidItem = 0
			}

			lastGuids = lastGuids[firstGuidItem:]
			combined := strings.Join(lastGuids, "||||")
			if bot.Fields.RSSLastGUIDs != combined {
				bot.Fields.RSSLastGUIDs = combined
				updatedFields["RSSLastGUIDs"] = combined
			}

			bot.Fields.LastCheckedAt = time.Now().UTC().Format(time.RFC3339)
			updatedFields["LastCheckedAt"] = bot.Fields.LastCheckedAt
		}
	}

	fmt.Printf("Bot Status Update: %#v\n", updatedFields)
	return updatedFields
}

func IsWeibo(url string) bool {
	return url[0:len("weibo:")] == "weibo:"
}

func Protocol(url string) string {
	if IsWeibo(url) {
		return ProtocolWeibo(url)
	}
	return url
}

func ProtocolWeibo(url string) string {
	return "http://rss.weibodangan.com/weibo/rss/" + url[len("weibo:"):] + "/"
}

func CreateGUID(url string, item *gofeed.Item) (*string, error) {
	if IsWeibo(url) {
		return CreateGUIDWeibo(url, item)
	}
	return &item.GUID, nil
}

func WeiboMid2Murl(mid string) (*string, error) {
	fullmid := leftPad2Len(mid, "0", 7*3)

	p1, err := baseconv.Convert(fullmid[0:7], baseconv.DigitsDec, baseconv.Digits62)
	if err != nil {
		return nil, err
	}

	p2, err := baseconv.Convert(fullmid[7:14], baseconv.DigitsDec, baseconv.Digits62)
	if err != nil {
		return nil, err
	}

	p3, err := baseconv.Convert(fullmid[14:21], baseconv.DigitsDec, baseconv.Digits62)
	if err != nil {
		return nil, err
	}

	p1 = leftPad2Len(p1, "0", 4)
	p2 = leftPad2Len(p2, "0", 4)
	p3 = leftPad2Len(p3, "0", 4)

	result := regexp.MustCompile(`^0+`).ReplaceAllLiteralString(p1+p2+p3, "")
	return &result, nil
}

func CreateGUIDWeibo(url string, item *gofeed.Item) (*string, error) {
	matches := regexp.MustCompile(`([0-9]+)/status([0-9]+)\.html`).FindStringSubmatch(item.GUID)
	if len(matches) == 0 {
		return nil, errors.New("cannot match guid: " + item.GUID)
	}
	uid := matches[1]
	mid := matches[2]
	murl, err := WeiboMid2Murl(mid)
	if err != nil {
		return nil, err
	}

	result := "http://weibo.com/" + uid + "/" + *murl
	return &result, nil
}

func CreateToot(url string, item *gofeed.Item, client *mastodon.Client, ctx *context.Context) (*mastodon.Toot, error) {
	if IsWeibo(url) {
		return CreateTootWeibo(url, item, client, ctx)
	}
	panic("not implemented")
}

func CreateTootWeibo(url string, item *gofeed.Item, client *mastodon.Client, ctx *context.Context) (*mastodon.Toot, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(item.Description))
	if err != nil {
		return nil, err
	}

	text := doc.Text()
	text = regexp.MustCompile("\\r\\n").ReplaceAllLiteralString(text, "\n")
	text = regexp.MustCompile("\\s{2,}").ReplaceAllLiteralString(text, " ")

	mediaIds := make([]int64, 0)

	nodes := make([]*goquery.Selection, 0)
	doc.Find("img[src*=\".sinaimg.cn/large/\"]").Each(func(i int, s *goquery.Selection) {
		nodes = append(nodes, s)
	})

	for _, s := range nodes {
		src := s.AttrOr("src", "")

		shortUrl, err := ShortURLSina(src, ctx)
		if err != nil {
			return nil, err
		}

		fmt.Printf("Processing Image: [%s] %s\n", *shortUrl, src)

		file, err := ioutil.TempFile(os.TempDir(), "mastodon-rss-bot")
		if err != nil {
			return nil, err
		}

		tmp := file.Name()

		resp, err := http.Get(src)
		if err != nil {
			return nil, err
		}

		data, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		_, err = file.Write(data)
		if err != nil {
			return nil, err
		}

		err = file.Close()
		if err != nil {
			return nil, err
		}

		defer os.Remove(tmp)

		fmt.Printf("Downloaded Image to temp file: %s\n", tmp)

		mime := http.DetectContentType(data)
		friendlyMimes := map[string]string{
			"image/x-icon":    "ICO",
			"image/bmp":       "BMP",
			"image/gif":       "GIF",
			"image/webp":      "WEBP",
			"image/png":       "PNG",
			"image/jpeg":      "JPEG",
			"application/ogg": "OGG",
			"audio/aiff":      "AIFF",
			"audio/midi":      "MIDI",
			"audio/mpeg":      "MP3",
			"audio/wave":      "WAV",
			"video/avi":       "AVI",
			"video/mp4":       "MP4",
			"video/webm":      "WEBM",
		}
		friendMime := friendlyMimes[mime]
		if friendMime == "" {
			friendMime = mime
		}
		filesize := len(data)

		descs := make([]string, 0)
		if filesize >= 1*1024*1024 {
			descs = append(descs, humanize.BigIBytes(big.NewInt(int64(filesize))))
		}
		if friendMime != "JPEG" {
			descs = append(descs, friendMime)
		}

		filedesc := ""
		if len(descs) > 0 {
			filedesc = " (" + strings.Join(descs, ", ") + ")"
		}

		width, height := getImageDimension(tmp)

		if width*height != 0 {
			if height/width > 1920/720 {
				text += "\n📜" + *shortUrl + filedesc
				continue
			}
		}
		text += "\n🖼️" + *shortUrl + filedesc

		if len(mediaIds) == 4 {
			continue
		}

		if filesize >= 6*1024*1024 {
			continue
		}

		attach, err := client.UploadMedia(*ctx, tmp)
		if err != nil {
			ReportError(err)
			continue
		}

		fmt.Printf("Uploaded Attachment: %#v\n", attach)
		mediaIds = append(mediaIds, attach.ID)
	}

	link, err := CreateGUIDWeibo(url, item)
	if err != nil {
		return nil, err
	}
	text += "\n🔗" + *link

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return nil, err
	}

	text += "\n⏰" + item.PublishedParsed.In(loc).Format("2006-01-02 15:04:05")

	return &mastodon.Toot{
		Status:     text,
		MediaIDs:   mediaIds,
		Visibility: "unlisted",
	}, nil
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func GetMastodonAuthenticityToken(server string, url string, jar *cookiejar.Jar) string {
	client := &http.Client{
		Jar: jar,
	}

	if url == "" {
		url = "/auth/sign_up"
	}

	resp, err := client.Get("https://" + server + url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromResponse(resp)
	if err != nil {
		panic(err)
	}

	var token string
	doc.Find("input[type=\"hidden\"][name=\"authenticity_token\"]").Each(func(i int, s *goquery.Selection) {
		token = s.AttrOr("value", "")
	})

	return token
}

func RegisterMastodonUser(server string, token string, username string, password string, email string, jar *cookiejar.Jar) {
	client := &http.Client{
		Jar: jar,
	}

	resp, err := client.PostForm("https://"+server+"/auth",
		url.Values{
			"utf8":                               {"✓"},
			"authenticity_token":                 {token},
			"user[account_attributes][username]": {username},
			"user[email]":                        {email},
			"user[password]":                     {password},
			"user[password_confirmation]":        {password},
		})

	if nil != err {
		panic(err)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	if nil != err {
		panic(err)
	}

	fmt.Println(string(body[:]))

}

func ShortURLSina(url string, ctx *context.Context) (*string, error) {
	req, err := http.NewRequest("GET", "http://api.t.sina.com.cn/short_url/shorten.json", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("source", "3271760578")
	q.Add("url_long", url)
	req.URL.RawQuery = q.Encode()

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	j := make([]map[string]interface{}, 0)
	err = json.Unmarshal(data, &j)
	if err != nil {
		return nil, err
	}

	short := j[0]["url_short"].(string)
	return &short, nil
}

func getImageDimension(imagePath string) (int, int) {
	file, err := os.Open(imagePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 0, 0
	}

	defer file.Close()

	image, _, err := image.DecodeConfig(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", imagePath, err)
		return 0, 0
	}

	return image.Width, image.Height
}

// https://github.com/DaddyOh/golang-samples/blob/master/pad.go
func rightPad2Len(s string, padStr string, overallLen int) string {
	var padCountInt int
	padCountInt = 1 + ((overallLen - len(padStr)) / len(padStr))
	var retStr = s + strings.Repeat(padStr, padCountInt)
	return retStr[:overallLen]
}

func leftPad2Len(s string, padStr string, overallLen int) string {
	var padCountInt int
	padCountInt = 1 + ((overallLen - len(padStr)) / len(padStr))
	var retStr = strings.Repeat(padStr, padCountInt) + s
	return retStr[(len(retStr) - overallLen):]
}
