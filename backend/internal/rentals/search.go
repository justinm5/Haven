// Package rentals queries real provider records. It has no seeded or fallback listings.
package rentals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrNotConfigured = errors.New("live listings are not configured")
var ErrUnavailable = errors.New("live listings temporarily unavailable")
var ErrQuota = errors.New("search capacity reached")
var ErrProviderAuth = errors.New("live listings credentials were rejected")
var ErrProviderPlan = errors.New("live listings provider plan does not allow this request")
var ErrProviderQuota = errors.New("live listings provider request limit reached")
var ErrProviderQuery = errors.New("live listings provider rejected the search parameters")

type Query struct {
	Area         string   `json:"area"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	MaxRentCents int64    `json:"max_rent_cents,omitempty"`
	MinBeds      *int     `json:"min_beds,omitempty"`
}
type History struct {
	Date      string `json:"date"`
	RentCents int64  `json:"rent_cents"`
	Event     string `json:"event"`
}
type Listing struct {
	ID           string    `json:"id"`
	Address      string    `json:"address"`
	City         string    `json:"city"`
	State        string    `json:"state"`
	RentCents    int64     `json:"rent_cents"`
	Beds         *float64  `json:"beds"`
	Baths        *float64  `json:"baths"`
	LastSeen     string    `json:"last_seen"`
	ListedAt     string    `json:"listed_at"`
	Source       string    `json:"source"`
	AgentWebsite string    `json:"agent_website,omitempty"`
	History      []History `json:"history"`
	Reasons      []string  `json:"reasons"`
}
type Results struct {
	Listings    []Listing `json:"listings"`
	RetrievedAt time.Time `json:"retrieved_at"`
	Source      string    `json:"source"`
	HasMore     bool      `json:"has_more"`
}
type cacheEntry struct {
	value   Results
	expires time.Time
}
type Client struct {
	Key      string
	HTTP     *http.Client
	endpoint string
	mu       sync.Mutex
	cache    map[string]cacheEntry
	month    string
	used     int
	// Per-process guard in addition to provider account controls. Never advertise
	// this as a durable billing cap: replicas and restarts require shared quotas.
	MaxMonthlyCalls int
}

func New(key string) *Client {
	return &Client{Key: strings.TrimSpace(key), HTTP: &http.Client{Timeout: 8 * time.Second}, endpoint: "https://api.rentcast.io/v1/listings/rental/long-term", cache: map[string]cacheEntry{}, MaxMonthlyCalls: 40}
}
func (c *Client) Configured() bool { return c != nil && c.Key != "" }

func (q Query) Parameters() (url.Values, error) {
	params := url.Values{"status": {"Active"}, "limit": {"50"}}
	if q.Latitude != nil || q.Longitude != nil {
		if q.Latitude == nil || q.Longitude == nil || math.IsNaN(*q.Latitude) || math.IsNaN(*q.Longitude) || math.IsInf(*q.Latitude, 0) || math.IsInf(*q.Longitude, 0) || *q.Latitude < -90 || *q.Latitude > 90 || *q.Longitude < -180 || *q.Longitude > 180 {
			return nil, fmt.Errorf("valid latitude and longitude required")
		}
		// Coarse coordinates only; do not pass exact device coordinates upstream.
		params.Set("latitude", strconv.FormatFloat(math.Round(*q.Latitude*100)/100, 'f', 2, 64))
		params.Set("longitude", strconv.FormatFloat(math.Round(*q.Longitude*100)/100, 'f', 2, 64))
		params.Set("radius", "10")
	} else {
		area := strings.TrimSpace(q.Area)
		if regexp.MustCompile(`^\d{5}(?:-\d{4})?$`).MatchString(area) {
			params.Set("zipCode", strings.SplitN(area, "-", 2)[0])
		} else {
			city, state, ok := splitCityState(area)
			if !ok {
				return nil, fmt.Errorf("enter a ZIP code or city and two-letter state, such as Amherst, MA")
			}
			params.Set("city", city)
			params.Set("state", state)
		}
	}
	if q.MaxRentCents < 0 || q.MaxRentCents > 100_000_000 {
		return nil, fmt.Errorf("invalid monthly budget")
	}
	if q.MaxRentCents > 0 {
		params.Set("price", "1:"+strconv.FormatFloat(float64(q.MaxRentCents)/100, 'f', 2, 64))
	}
	if q.MinBeds != nil {
		if *q.MinBeds < 0 || *q.MinBeds > 10 {
			return nil, fmt.Errorf("invalid bedroom preference")
		}
		if *q.MinBeds == 0 {
			params.Set("bedrooms", "0")
		} else {
			params.Set("bedrooms", fmt.Sprintf("%d:10", *q.MinBeds))
		}
	}
	return params, nil
}

var cityStatePattern = regexp.MustCompile(`(?i)^(.+?)(?:,\s*|\s+)([a-z]{2})$`)

func splitCityState(area string) (string, string, bool) {
	if len(area) > 120 {
		return "", "", false
	}
	m := cityStatePattern.FindStringSubmatch(strings.TrimSpace(area))
	if len(m) != 3 {
		return "", "", false
	}
	city := strings.Join(strings.Fields(strings.TrimSpace(m[1])), " ")
	if len(city) < 2 {
		return "", "", false
	}
	// Keep conventional capitalization while preserving acronyms and punctuation.
	words := strings.Fields(strings.ToLower(city))
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " "), strings.ToUpper(m[2]), true
}

type providerListing struct {
	ID               string   `json:"id"`
	FormattedAddress string   `json:"formattedAddress"`
	City             string   `json:"city"`
	State            string   `json:"state"`
	Status           string   `json:"status"`
	Price            float64  `json:"price"`
	Bedrooms         *float64 `json:"bedrooms"`
	Bathrooms        *float64 `json:"bathrooms"`
	LastSeenDate     string   `json:"lastSeenDate"`
	ListedDate       string   `json:"listedDate"`
	ListingAgent     struct {
		Website string `json:"website"`
	} `json:"listingAgent"`
	History map[string]struct {
		Price      float64 `json:"price"`
		Event      string  `json:"event"`
		ListedDate string  `json:"listedDate"`
	} `json:"history"`
}

func normalize(raw []providerListing, q Query, now time.Time) Results {
	result := Results{Listings: []Listing{}, RetrievedAt: now.UTC(), Source: "RentCast", HasMore: len(raw) >= 50}
	seen := map[string]bool{}
	for _, r := range raw {
		if r.ID == "" || r.FormattedAddress == "" || r.Status != "Active" || r.Price <= 0 || math.IsNaN(r.Price) || math.IsInf(r.Price, 0) || r.Price > 1_000_000 || seen[r.ID] {
			continue
		}
		price := int64(math.Round(r.Price * 100))
		if q.MaxRentCents > 0 && price > q.MaxRentCents {
			continue
		}
		if q.MinBeds != nil && (r.Bedrooms == nil || (*q.MinBeds == 0 && *r.Bedrooms != 0) || (*q.MinBeds > 0 && *r.Bedrooms < float64(*q.MinBeds))) {
			continue
		}
		seen[r.ID] = true
		l := Listing{ID: r.ID, Address: r.FormattedAddress, City: r.City, State: r.State, RentCents: price, Beds: r.Bedrooms, Baths: r.Bathrooms, LastSeen: r.LastSeenDate, ListedAt: r.ListedDate, Source: "RentCast", History: []History{}, Reasons: []string{}}
		if q.MaxRentCents > 0 {
			l.Reasons = append(l.Reasons, "Within your rent budget")
		}
		if q.MinBeds != nil {
			if *q.MinBeds == 0 {
				l.Reasons = append(l.Reasons, "Studio")
			} else {
				l.Reasons = append(l.Reasons, fmt.Sprintf("%d+ bedrooms", *q.MinBeds))
			}
		}
		if len(l.Reasons) == 0 {
			l.Reasons = append(l.Reasons, "In your search area")
		}
		if u, err := url.Parse(r.ListingAgent.Website); err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil {
			l.AgentWebsite = u.String()
		}
		for day, h := range r.History {
			if _, err := time.Parse("2006-01-02", day); err == nil && h.Price > 0 && h.Price <= 1_000_000 {
				l.History = append(l.History, History{Date: day, RentCents: int64(math.Round(h.Price * 100)), Event: h.Event})
			}
		}
		sort.Slice(l.History, func(i, j int) bool { return l.History[i].Date < l.History[j].Date })
		result.Listings = append(result.Listings, l)
	}
	// Practical ordering: lower listed rent, then more recently seen records.
	sort.SliceStable(result.Listings, func(i, j int) bool {
		a, b := result.Listings[i], result.Listings[j]
		if a.RentCents == b.RentCents {
			return a.LastSeen > b.LastSeen
		}
		return a.RentCents < b.RentCents
	})
	return result
}
func (c *Client) Search(ctx context.Context, q Query) (Results, error) {
	params, err := q.Parameters()
	if err != nil {
		return Results{}, err
	}
	if !c.Configured() {
		return Results{}, ErrNotConfigured
	}
	// Collapse identical concurrent searches and bound the cache. A single request
	// consumes one provider call; never retry paid requests automatically.
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	key := params.Encode()
	if e, ok := c.cache[key]; ok && e.expires.After(now) {
		return e.value, nil
	}
	month := now.UTC().Format("2006-01")
	if c.month != month {
		c.month = month
		c.used = 0
	}
	if c.MaxMonthlyCalls <= 0 || c.used >= c.MaxMonthlyCalls {
		return Results{}, ErrQuota
	}
	c.used++
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"?"+key, nil)
	if err != nil {
		return Results{}, ErrUnavailable
	}
	req.Header.Set("X-Api-Key", c.Key)
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Results{}, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		// Record only the HTTP status; never log API keys, query values, or response bodies.
		slog.Warn("live rental provider request failed", "status", res.StatusCode)
	}
	if res.StatusCode == http.StatusUnauthorized {
		return Results{}, ErrProviderAuth
	}
	if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusPaymentRequired {
		return Results{}, ErrProviderPlan
	}
	if res.StatusCode == http.StatusTooManyRequests {
		return Results{}, ErrProviderQuota
	}
	if res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusUnprocessableEntity {
		return Results{}, ErrProviderQuery
	}
	if res.StatusCode != http.StatusOK {
		return Results{}, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return Results{}, ErrUnavailable
	}
	var raw []providerListing
	if json.Unmarshal(body, &raw) != nil {
		return Results{}, ErrUnavailable
	}
	result := normalize(raw, q, now)
	if len(c.cache) >= 256 {
		for k, e := range c.cache {
			if e.expires.Before(now) {
				delete(c.cache, k)
			}
		}
		if len(c.cache) >= 256 {
			c.cache = map[string]cacheEntry{}
		}
	}
	c.cache[key] = cacheEntry{value: result, expires: now.Add(10 * time.Minute)}
	return result, nil
}
