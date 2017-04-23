package main

import (
	"context"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/fabioberger/airtable-go"
	"github.com/jmcvetta/randutil"
	"github.com/mattn/go-mastodon"
	"github.com/mmcdole/gofeed"
	"github.com/vincent-petithory/dataurl"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "image/jpeg"
	_ "image/png"
	_ "image/gif"
	"image"
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

func (bot *Bot) Process() map[string]interface{} {
	ctx := context.Background()
	updatedFields := map[string]interface{}{}
	if !bot.Fields.Enabled {
		return updatedFields
	}

	fmt.Printf("%#v\n", bot)

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
			status, err := c.GetAccountStatuses(ctx, account.ID)
			if err != nil {
				panic(err)
			}

			for _, item := range status {
				err := c.DeleteStatus(ctx, item.ID)
				if err != nil {
					panic(err)
				}
			}

			bot.Fields.RSSLastGUIDs = ""
			bot.Fields.CleanAndReset = false
			updatedFields["RSSLastGUIDs"] = bot.Fields.RSSLastGUIDs
			updatedFields["CleanAndReset"] = bot.Fields.CleanAndReset
		}

		{
			resp, err := http.Get(bot.Fields.RSSUrl)
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
				fmt.Printf("%#v\n", item)

				if stringInSlice(item.Link, lastGuids) {
					continue
				}
				lastGuids = append(lastGuids, item.Link)

				doc, err := goquery.NewDocumentFromReader(strings.NewReader(item.Description))
				if err != nil {
					panic(err)
				}

				text := doc.Text()
				text = regexp.MustCompile("\\r\\n").ReplaceAllLiteralString(text, "\n")
				text = regexp.MustCompile("\\s{2,}").ReplaceAllLiteralString(text, " ")

				loc, err := time.LoadLocation("Asia/Shanghai")
				if err != nil {
					panic(err)
				}

				fmt.Printf(text)

				mediaIds := make([]int64, 0)
				extraCount := 0

				doc.Find(".wgt_img").Each(func(i int, s *goquery.Selection) {
					if len(mediaIds) == 4 {
						extraCount += 1
						return
					}

					src := s.AttrOr("src", "")
					fmt.Printf("%#v\n", src)

					file, err := ioutil.TempFile(os.TempDir(), "mastodon-rss-bot")
					if err != nil {
						panic(err)
					}

					tmp := file.Name()

					resp, err := http.Get(src)
					if err != nil {
						panic(err)
					}
					defer resp.Body.Close()

					data, err := ioutil.ReadAll(resp.Body)
					if err != nil {
						panic(err)
					}

					_, err = file.Write(data)
					if err != nil {
						panic(err)
					}

					err = file.Close()
					if err != nil {
						panic(err)
					}

					fmt.Printf("%s\n", tmp)

					width, height := getImageDimension(tmp)

					if width * height != 0 {
						if height / width > 1920 / 720 {
							text += "\n📜" + src
							return
						}
					}

					attach, err := c.UploadMedia(ctx, tmp)
					if err != nil {
						panic(err)
					}

					fmt.Printf("%#v\n", attach)
					text += "\n🖼️" + src //attach.TextURL
					mediaIds = append(mediaIds, attach.ID)

					err = os.Remove(tmp)
					if err != nil {
						panic(err)
					}
				})

				if extraCount > 0 {
					text += "\n..🖼️+️" + strconv.FormatInt(int64(extraCount), 10)
				}
				text += "\n🔗" + item.Link
				text += "\n⏰" + item.PublishedParsed.In(loc).Format("2006-01-02 15:04:05")
				text += "\n"

				toot := &mastodon.Toot{
					Status:   text,
					MediaIDs: mediaIds,
					Visibility: "unlisted",
				}
				fmt.Printf("%#v\n", toot)

				_, err = c.PostStatus(ctx, toot)
				if err != nil {
					panic(err)
				}

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
			bot.Fields.RSSLastGUIDs = strings.Join(lastGuids, "||||")
			updatedFields["RSSLastGUIDs"] = bot.Fields.RSSLastGUIDs

			bot.Fields.LastCheckedAt = time.Now().UTC().Format(time.RFC3339)
			updatedFields["LastCheckedAt"] = bot.Fields.LastCheckedAt
		}
	}

	fmt.Printf("%#v\n", updatedFields)
	return updatedFields
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