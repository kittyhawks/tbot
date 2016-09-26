package scraper

import (
	"github.com/AskUbuntu/tbot/config"
	"github.com/PuerkitoBio/goquery"

	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
)

// Scraper regularly scrapes the chat transcript for the current and previous
// day for messages that match the criteria in use. Once a message matches, it
// is added to the list of candidates for tweeting. The IDs of messages that
// are used is kept to prevent duplicates.
type Scraper struct {
	data      *data
	settings  *settings
	closeChan chan bool
}

func atoi(str string) int {
	v, _ := strconv.Atoi(str)
	return v
}

func (s *Scraper) scrapePage(document *goquery.Document) (earliestID int, messages []*Message) {
	s.data.Lock()
	messagesUsed := s.data.MessagesUsed
	s.data.Unlock()
	s.settings.Lock()
	var (
		pollURL       = s.settings.PollURL
		minStars      = s.settings.MinStars
		matchingWords = s.settings.MatchingWords
	)
	s.settings.Unlock()
	document.Each(func(i int, selection *goquery.Selection) {
		var (
			link = selection.Find("a[name]")
			id   = atoi(link.AttrOr("name", ""))
		)
		if id == 0 {
			return
		}
		if earliestID == 0 {
			earliestID = id
		}
		var (
			body       = selection.Find(".content").Text()
			stars      = atoi(selection.Find(".stars .times").Text())
			foundMatch = false
		)
		for _, w := range matchingWords {
			if strings.Contains(strings.ToLower(body), strings.ToLower(w)) {
				foundMatch = true
			}
		}
		if foundMatch || stars >= minStars {
			messageUsed := false
			for _, m := range messagesUsed {
				if m == id {
					messageUsed = true
				}
			}
			if !messageUsed {
				m := &Message{
					ID:     id,
					URL:    fmt.Sprintf("%s%s", pollURL, link.AttrOr("href", "")),
					Body:   body,
					Author: selection.Parent().Find(".signature .username").Text(),
					Stars:  stars,
				}
				messages = append(messages, m)
			}
		}
	})
	return
}

func (s *Scraper) scrape() error {
	s.settings.Lock()
	var (
		pollURL    = s.settings.PollURL
		pollRoomID = s.settings.PollRoomID
	)
	s.settings.Unlock()
	document, err := goquery.NewDocument(
		fmt.Sprintf("%s/transcript/%d", pollURL, pollRoomID),
	)
	if err != nil {
		return err
	}
	var (
		path       = document.Find("a[rel=prev]").First().AttrOr("href", "")
		earliestID = 0
		messages   = []*Message{}
	)
	for path != "" {
		document, err = goquery.NewDocument(
			fmt.Sprintf("%s%s", pollURL, path),
		)
		if err != nil {
			return err
		}
		newEarliestID, newMessages := s.scrapePage(document)
		if earliestID == 0 {
			earliestID = newEarliestID
		}
		messages = append(messages, newMessages...)
		selection := document.Find(".pager .current").NextFiltered("a")
		if selection.Length() == 0 {
			selection = document.Find("a[rel=prev]").NextFiltered("a")
		}
		path = selection.AttrOr("href", "")
	}
	s.data.Lock()
	s.data.LastScrape = time.Now()
	s.data.EarliestID = earliestID
	s.data.Messages = messages
	for i := len(s.data.MessagesUsed) - 1; i >= 0; i-- {
		if s.data.MessagesUsed[i] < earliestID {
			s.data.MessagesUsed = append(
				s.data.MessagesUsed[:i],
				s.data.MessagesUsed[i+1:]...,
			)
		}
	}
	if err := s.data.save(); err != nil {
		return err
	}
	s.data.Unlock()
	return nil
}

func (s *Scraper) run() {
	for {
		s.data.Lock()
		lastScrape := s.data.LastScrape
		s.data.Unlock()
		s.settings.Lock()
		pollFrequency := s.settings.PollFrequency
		s.settings.Unlock()
		var (
			now      = time.Now()
			duration = time.Duration(pollFrequency)
			diff     = lastScrape.Add(duration * time.Minute).Sub(now)
		)
		if diff <= 0 {
			// TODO: log scrape error
			s.scrape()
			diff = duration
		}
		var (
			timer = time.NewTimer(diff)
			quit  = false
		)
		select {
		case <-timer.C:
		case <-s.closeChan:
			quit = true
		}
		if !timer.Stop() {
			<-timer.C
		}
		if quit {
			break
		}
	}
	close(s.closeChan)
}

// NewScraper creates a new scraper.
func NewScraper(c *config.Config) (*Scraper, error) {
	s := &Scraper{
		data:      &data{name: path.Join(c.DataPath, "scraper_data.json")},
		settings:  &settings{name: path.Join(c.DataPath, "scraper_settings.json")},
		closeChan: make(chan bool),
	}
	if err := s.data.load(); err != nil {
		return nil, err
	}
	if err := s.settings.load(); err != nil {
		return nil, err
	}
	go s.run()
	return s, nil
}

// Messages retrieves the current list of matching messages.
func (s *Scraper) Messages() []*Message {
	s.data.Lock()
	defer s.data.Unlock()
	return s.data.Messages
}

// Use removes the message from the list in preparation for use. This will also
// cause the message to be ignored in future scrapes.
func (s *Scraper) Use(id int) (*Message, error) {
	s.data.Lock()
	defer s.data.Unlock()
	var message *Message
	for i := len(s.data.Messages) - 1; i >= 0; i-- {
		m := s.data.Messages[i]
		if m.ID == id {
			message = m
			s.data.Messages = append(
				s.data.Messages[:i],
				s.data.Messages[i+1:]...,
			)
		}
	}
	if message == nil {
		return nil, errors.New("Invalid message index")
	}
	s.data.MessagesUsed = append(s.data.MessagesUsed, message.ID)
	s.data.save()
	return message, nil
}

// Settings retrieves the current settings for the scraper.
func (s *Scraper) Settings() Settings {
	s.settings.Lock()
	defer s.settings.Unlock()
	return s.settings.Settings
}

// SetSettings stores the current settings for the scraper.
func (s *Scraper) SetSettings(settings Settings) {
	s.settings.Lock()
	defer s.settings.Unlock()
	name := s.settings.name
	s.settings.Settings = settings
	s.settings.name = name
}

// Close shuts down the scraper and waits for it to exit.
func (s *Scraper) Close() {
	s.closeChan <- true
	<-s.closeChan
}
