package firescrewServe

import (
	"testing"
	"time"
)

func TestParseDateRangePrompt(t *testing.T) {
	tests := []struct {
		prompt    string
		startTime time.Time
		endTime   time.Time
		hasError  bool
	}{
		{
			prompt:    "people today from 9am to 1pm",
			startTime: time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 9, 0, 0, 0, time.Local),
			endTime:   time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 13, 0, 0, 0, time.Local),
			hasError:  false,
		},
		{
			prompt:    "people today from 9am to 1pm",
			startTime: time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 9, 0, 0, 0, time.Local),
			endTime:   time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 13, 0, 0, 0, time.Local),
			hasError:  false,
		},
		{
			prompt:    "people between 2pm and 4pm",
			startTime: time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 14, 0, 0, 0, time.Local),
			endTime:   time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 16, 0, 0, 0, time.Local),
			hasError:  false,
		},
		{
			prompt:    "yesterday 5pm",
			startTime: time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day()-1, 17, 0, 0, 0, time.Local),
			endTime:   time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day()-1, 18, 0, 0, 0, time.Local),
			hasError:  false,
		}, {
			prompt:    "between july 7th 5pm and july 7th 6pm",
			startTime: time.Date(time.Now().Year(), time.July, 7, 17, 0, 0, 0, time.Local),
			endTime:   time.Date(time.Now().Year(), time.July, 7, 18, 0, 0, 0, time.Local),
			hasError:  false,
		},
		{
			prompt:    "from july 7th 5pm to july 7th 6pm",
			startTime: time.Date(time.Now().Year(), time.July, 7, 17, 0, 0, 0, time.Local),
			endTime:   time.Date(time.Now().Year(), time.July, 7, 18, 0, 0, 0, time.Local),
			hasError:  false,
		}, {
			prompt:    "july 4th 9am",
			startTime: time.Date(time.Now().Year(), time.July, 4, 9, 0, 0, 0, time.Local),
			endTime:   time.Date(time.Now().Year(), time.July, 4, 10, 0, 0, 0, time.Local),
			hasError:  false,
		},
	}

	for _, test := range tests {
		startTime, endTime, err := ParseDateRangePrompt(test.prompt)
		if (err != nil) != test.hasError {
			t.Errorf("Unexpected error for prompt %q: %v", test.prompt, err)
		}
		if !startTime.Equal(test.startTime) || !endTime.Equal(test.endTime) {
			t.Errorf("For prompt %q, expected start time %v and end time %v, but got start time %v and end time %v", test.prompt, test.startTime, test.endTime, startTime, endTime)
		}
	}
}
