# mastodon-rss-bot

**RSS is not supported at this moment. Only Weibo.com forwarding is working at this moment (LOL)**

## Configuration

### Setup

Currently this bot utilizes [Airtable][airtable] (aff link) for storing bot configuration and statuses.

You should create a Airtable base and then create a table named "Bot List" (name must be matched exactly) with the following columns.
* For a `string` column, choose *Single line text*.
* For a `string (multiline)` column, choose *Long text*.
* For a `bool` column, choose *Checkbox*.

```
Name                       string
Enabled                    bool
Registered                 bool
Activated                  bool
BotInstance                string
BotUsername                string
BotEmail                   string
BotName                    string
BotBio                     string
BotAvatarUrl               string
BotApplicationName         string
BotApplicationUrl          string
RSSUrl                     string
RSSTemplate                string
BotApplicationId           string
BotApplicationClientId     string
BotApplicationClientSecret string
BotPassword                string
BotAvatarUploaded          bool
LastCheckedAt              string
LastPostedAt               string
RSSLastGUIDs               string (multiline)
CleanAndReset              bool
```

![](http://i.imgur.com/7Mt5PDF.png)

### Usage

1. Create a record like this: (leaving other fields empty)
   ```
   Enabled: false
   Registered: false
   Activated: false
   BotInstance: pawoo.net
   BotUsername: oh-my-yummy-bot
   BotEmail: oh-my-yummy-bot@oh-your-domain.com
   BotName: Oh My Yummy Bot
   BotBio: Another mastodon-rss-bot bot maintained by @nobody
   BotAvatarUrl: https://media.mstdn.jp/images/accounts/avatars/000/125/787/original/408fca009ef382f0.png
   BotApplicationName: mastodon-rss-bot
   BotApplicationUrl: https://github.com/orzFly/mastodon-rss-bot
   RSSUrl: weibo:1113218211
   ```
1. Set `Enabled` to `true`.   
1. Run `mastodon-rss-bot`. The bot will register the account for you. You will find `Registered` changed to `true` in Airtable.
1. Check your e-mail inbox and confirm the account.
1. Set `Activated` to `true`.
1. Set up a cron job to run `mastodon-rss-bot`.
1. Enjoy™
## Environment Variables
* `AIRTABLE_API_KEY` Airtable API Key (A random string looks like `keyXXXXXXXX`)
* `AIRTABLE_BASE_ID` Airtable Base ID (A random string looks like `appXXXXXXXX`)
* `RAVEN_DSN` [Sentry][sentry] Integration (A random string looks like `https://<key>:<secret>@sentry.io/<project>`)

## Building
1. `glide install`
1. `go build -o mastodon-rss-bot`
1. `AIRTABLE_API_KEY=keyXXXXXX AIRTABLE_BASE_ID=appXXXXXX ./mastodon-rss-bot`

## License
    Copyright (C) 2017  Yeechan Lu

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU Affero General Public License as published
    by the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU Affero General Public License for more details.

    You should have received a copy of the GNU Affero General Public License
    along with this program.  If not, see <http://www.gnu.org/licenses/>.

  [airtable]: https://airtable.com/invite/r/APgl0VxI
  [sentry]: https://sentry.io/welcome/