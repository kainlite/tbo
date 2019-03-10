package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
)

// Version
var Version string

// Environment
var Env string

// Page JSON object
type Page struct {
	Version  string    `json:"version"`
	Title    string    `json:"title"`
	BaseURL  string    `json:"home_page_url"`
	FeedURL  string    `json:"feed_url"`
	Articles []Article `json:"items"`
}

// Article JSON object
type Article struct {
	ID        string   `json:"id"`
	URL       string   `json:"url"`
	Title     string   `json:"title"`
	Content   string   `json:"content_html"`
	Tags      []string `json:tags`
	Published string   `json:"date_published"`
}

// Twitter Access
type Twitter struct {
	config      *oauth1.Config
	token       *oauth1.Token
	httpClient  *http.Client
	client      *twitter.Client
	tweetFormat string
	screenName  string
	lastTweets  int
}

func (t *Twitter) Setup() {
	log.Debug("Setting up twitter client")
	var twitterAccessKey string
	var twitterAccessSecret string
	var twitterConsumerKey string
	var twitterConsumerSecret string

	// Get the access keys from ENV
	twitterAccessKey = os.Getenv("TWITTER_ACCESS_KEY")
	twitterAccessSecret = os.Getenv("TWITTER_ACCESS_SECRET")
	twitterConsumerKey = os.Getenv("TWITTER_CONSUMER_KEY")
	twitterConsumerSecret = os.Getenv("TWITTER_CONSUMER_SECRET")
	twitterScreenName := os.Getenv("TWITTER_SCREEN_NAME")
	twitterLastTweets := os.Getenv("TWITTER_LAST_TWEETS")

	if twitterScreenName == "" {
		log.Fatalf("Twitter screen name cannot be null")
	}

	if twitterConsumerKey == "" {
		log.Fatal("Twitter consumer key can not be null")
	}

	if twitterConsumerSecret == "" {
		log.Fatal("Twitter consumer secret can not be null")
	}

	if twitterAccessKey == "" {
		log.Fatal("Twitter access key can not be null")
	}

	if twitterAccessSecret == "" {
		log.Fatal("Twitter access secret can not be null")
	}

	log.Debug("Setting up oAuth for twitter")
	// Setup the new oauth client
	t.config = oauth1.NewConfig(twitterConsumerKey, twitterConsumerSecret)
	t.token = oauth1.NewToken(twitterAccessKey, twitterAccessSecret)
	t.httpClient = t.config.Client(oauth1.NoContext, t.token)

	// Twitter client
	t.client = twitter.NewClient(t.httpClient)

	// Set the screen name for later use
	t.screenName = twitterScreenName

	// Set the amount of tweets to look back
	t.lastTweets, _ = strconv.Atoi(twitterLastTweets)

	// This is the format of the tweet
	t.tweetFormat = "%s: %s %s - TBO"
	log.Debug("Twitter client setup complete")
}

func getHashTags(tags []string) string {
	var hashTags strings.Builder

	for _, tag := range tags {
		fmt.Fprintf(&hashTags, "#%v ", tag)
	}

	return hashTags.String()
}

func (t *Twitter) GetTweetString(article Article) string {
	hashTags := getHashTags(article.Tags)
	return fmt.Sprintf(t.tweetFormat, article.Title, hashTags, article.URL)
}

// Send the tweet
func (t *Twitter) Send(article Article) {
	log.Debug("Sending tweet")
	if Env != "production" {
		log.Infof("Non production mode, would've tweeted: %s", t.GetTweetString(article))
	}
	if Env == "production" {
		log.Infof("Sending tweet: %s", t.GetTweetString(article))
		if _, _, err := t.client.Statuses.Update(t.GetTweetString(article), nil); err != nil {
			log.Fatalf("Error sending tweet to twitter: %s", err)
		}
	}
}

// Get a random article from the feed
func (t *Twitter) PickArticle(article Article) bool {
	log.Debug("Checking to see if the tweet appeared in the last %d tweets", t.lastTweets)

	tweets, _, err := t.client.Timelines.UserTimeline(&twitter.UserTimelineParams{
		ScreenName: t.screenName,
		Count:      t.lastTweets,
		TweetMode:  "extended",
	})

	if err != nil {
		log.Fatalf("Error getting last %d tweets from user: %s", t.lastTweets, err)
	}

	for _, tweet := range tweets {
		fmt.Printf("Comparing tweet: %s, with: %s\n", tweet.FullText, article.Title)
		if strings.Contains(tweet.FullText, article.Title) {
			return true
		}
	}

	fmt.Println("false")
	return false
}

// Twitter API constant
var tw Twitter

// GetArticle()
func GetArticle() Article {
	url := "https://techsquad.rocks/index.json"

	// Setup a new HTTP Client with 2 second timeout
	httpClient := http.Client{
		Timeout: time.Second * 3,
	}

	// Create a new HTTP Request
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		// An error has occurred that we can't recover from, bail.
		log.Fatalf("Error occurred creating new request: %s", err)
	}

	// Set the user agent to tbo <verion> - twitter.com/kainlite
	req.Header.Set("User-Agent", fmt.Sprintf("TBO %s - twitter.com/kainlite", Version))

	// Tell the remote server to send us JSON
	req.Header.Set("Accept", "application/json")

	// We're only going to try maxRetries times, otherwise we'll fatal out.
	// Execute the request
	log.Debugf("Attempting request to %s", req)
	res, getErr := httpClient.Do(req)
	if getErr != nil {
		// We got an error, lets bail out, we can't do anything more
		log.Fatalf("Error occurred retrieving article from API: %s", getErr)
	}

	// BGet the body from the result
	body, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		// This shouldn't happen, but if it does, error out.
		log.Fatalf("Error occurred reading from result body: %s", readErr)
	}

	var page Page
	if err := json.Unmarshal(body, &page); err != nil {
		// Invalid JSON was received, bail out
		log.Fatalf("Error occurred decoding article: %s", err)
	}

	invalidArticle := true
	try := 0
	maxRetries, _ := strconv.Atoi(os.Getenv("MAX_RETRIES"))

	var article Article
	for invalidArticle {
		rand.Seed(time.Now().UnixNano())
		randomInt := rand.Intn(len(page.Articles))
		article = page.Articles[randomInt]

		fmt.Printf("Article id: %+v\n", randomInt)
		// check to make sure the tweet hasn't been sent before
		if tw.PickArticle(article) {
			try += 1
			continue
		}

		// If we get here we've found a tweet, exit the loop
		invalidArticle = false

		if try >= maxRetries {
			log.Fatal("Exiting after attempts to retrieve article failed.")
		}
	}

	// Return the valid article response
	return article
}

// HandleRequest - Handle the incoming Lambda request
func HandleRequest() {
	log.Debug("Started handling request")
	tw.Setup()
	article := GetArticle()

	// Send tweet
	tw.Send(article)
}

// Set the local environment
func setRunningEnvironment() {
	// Get the environment variable
	switch os.Getenv("APP_ENV") {
	case "production":
		Env = "production"
	case "development":
		Env = "development"
	default:
		Env = "development"
	}

	if Env != "production" {
		Version = Env
	}
}

func shutdown() {
	log.Info("Shutdown request registered")
}

func init() {
	// Set the environment
	setRunningEnvironment()

	// Set logging configuration
	log.SetFormatter(&log.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})

	log.SetReportCaller(true)
	switch Env {
	case "development":
		log.SetLevel(log.DebugLevel)
	case "production":
		log.SetLevel(log.ErrorLevel)
	default:
		log.SetLevel(log.InfoLevel)
	}
}

func main() {
	// Start the bot
	log.Debug("Starting main")
	log.Printf("TBO %s", Version)
	if Env == "production" {
		lambda.Start(HandleRequest)
	} else {
		if err := godotenv.Load(); err != nil {
			log.Fatal("Error loading .env file")
		}
		HandleRequest()
	}
}
