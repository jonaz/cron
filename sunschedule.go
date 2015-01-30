package cron

import (
	"fmt"
	"strings"
	"time"

	"github.com/jonaz/astrotime"
)

type SunSchedule struct {
	state  string
	fields []string
}

func NewSunSchedule(state string) *SunSchedule {
	//Remove @ in the beginning
	state = state[1:]
	fields := strings.Fields(state)

	//Fix empty fields and set them to *
	if len(fields) == 1 {
		fields = append(fields, "*")
		fields = append(fields, "*")
		fields = append(fields, "*")
	}
	if len(fields) == 2 {
		fields = append(fields, "*")
		fields = append(fields, "*")
	}
	if len(fields) == 3 {
		fields = append(fields, "*")
	}

	return &SunSchedule{state: fields[0], fields: fields[1:]}
}

// next is used for getting the day when the next run shall be.
// So it can be fed to astrotime for checking sun on the correct day
func (s *SunSchedule) next() time.Time {

	schedule := &SpecSchedule{
		Second: getField("*", seconds),
		Minute: getField("*", minutes),
		Hour:   getField("*", hours),
		Dom:    getField(s.fields[0], dom),
		Month:  getField(s.fields[1], months),
		Dow:    getField(s.fields[2], dow),
	}

	return schedule.Next(time.Now().Local())
}

func (s *SunSchedule) Next(t time.Time) time.Time {
	basetime := s.next()

	fmt.Println(basetime)

	switch s.state {
	case "sunset":
		return astrotime.NextSunset(basetime, float64(56.878333), float64(14.809167))
	case "sunrise":
		return astrotime.NextSunrise(basetime, float64(56.878333), float64(14.809167))
	case "dusk":
		return astrotime.NextDusk(basetime, float64(56.878333), float64(14.809167), astrotime.CIVIL_DUSK)
	case "dawn":
		return astrotime.NextDawn(basetime, float64(56.878333), float64(14.809167), astrotime.CIVIL_DAWN)
	}

	return time.Time{}
}
