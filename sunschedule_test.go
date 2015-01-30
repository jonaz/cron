package cron

import (
	"testing"
	"time"
)

func TestSunScheduleNextSunset(t *testing.T) {

	s := NewSunSchedule("@sunset")
	t.Log(s)

	t1 := s.Next(time.Now())
	t.Log(t1)
}

func TestSunScheduleNextSunrise(t *testing.T) {

	s := NewSunSchedule("@sunrise")
	t.Log(s)

	t1 := s.Next(time.Now())
	t.Log(t1)
}

func TestSunScheduleNextSunriseOtherDate(t *testing.T) {

	s := NewSunSchedule("@sunrise * * 0")
	t.Log(s)

	t1 := s.Next(time.Now())
	t.Log(t1)
}
