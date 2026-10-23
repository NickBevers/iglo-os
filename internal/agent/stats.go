package agent

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// ErrNoStats is the answer for an app that can't report its stats yet.
var ErrNoStats = errors.New("this app can't report its stats yet")

// AppStats is what an app holds and does right now, for the dashboard. Each
// app fills it in its own way: the music app from its downloads, the photo
// app from Immich, the inbox from FileFlows.
type AppStats struct {
	// Sample marks synthetic statistics from the local mock and recorded demo.
	Sample   bool       `json:"sample,omitempty"`
	Figures  []Figure   `json:"figures"`
	Activity []Activity `json:"activity"`
}

// Figure is one number, like 1534 songs. With the unit "bytes", the
// dashboard shows it as a size.
type Figure struct {
	Label string `json:"label"`
	Value int64  `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

// Activity is something the app does now or did lately, like a download.
// Progress is 0 to 100, or -1 when there is none.
type Activity struct {
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Progress int    `json:"progress"`
}

func (c *Client) AppStats(ctx context.Context, app string, vmid int) (AppStats, error) {
	var stats AppStats
	err := c.do(ctx, http.MethodGet, "/v1/apps/"+url.PathEscape(app)+"/stats?vmid="+strconv.Itoa(vmid), nil, &stats)

	return stats, err
}
