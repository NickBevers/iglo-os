package events

import "testing"

func TestHubPassesTopicsWithoutBlocking(t *testing.T) {
	hub := NewHub()
	first, stopFirst := hub.Subscribe()
	second, stopSecond := hub.Subscribe()
	defer stopSecond()

	hub.Publish(Apps)
	if <-first != Apps || <-second != Apps {
		t.Fatal("a page missed the topic")
	}

	// A page that doesn't read never blocks the others.
	for range 100 {
		hub.Publish(Gaming)
	}
	if len(second) != cap(second) {
		t.Fatalf("buffered %d", len(second))
	}

	stopFirst()
	hub.Publish(Music)
	select {
	case topic := <-first:
		if topic != Gaming {
			t.Fatalf("an ended page got %q", topic)
		}
	default:
	}

	// A service without a hub may publish too.
	var none *Hub
	none.Publish(Apps)
}
