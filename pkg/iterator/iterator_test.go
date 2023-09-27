// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
//
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package iterator

import (
	"testing"
	"time"

	"golang.org/x/exp/slices"
)

// dummy query provider can be instantiated with counts
type DummyQueryProvider struct {
	state map[string]int
}

func (d *DummyQueryProvider) StartTime() time.Time {
	keys := make([]time.Time, 0, len(d.state))
	for k := range d.state {
		parsedTime, _ := time.Parse(time.RFC822Z, k)
		keys = append(keys, parsedTime)
	}
	return slices.MinFunc(keys, func(a time.Time, b time.Time) int {
		return a.Compare(b)
	})
}

func (d *DummyQueryProvider) EndTime() time.Time {
	keys := make([]time.Time, 0, len(d.state))
	for k := range d.state {
		parsedTime, _ := time.Parse(time.RFC822Z, k)
		keys = append(keys, parsedTime)
	}
	maxTime := slices.MaxFunc(keys, func(a time.Time, b time.Time) int {
		return a.Compare(b)
	})

	return maxTime.Add(time.Minute)
}

func (*DummyQueryProvider) QueryRunnerFunc() func(time.Time, time.Time) ([]map[string]interface{}, error) {
	return func(t1, t2 time.Time) ([]map[string]interface{}, error) {
		return make([]map[string]interface{}, 0), nil
	}
}

func (d *DummyQueryProvider) HasDataFunc() func(time.Time, time.Time) bool {
	return func(t1, t2 time.Time) bool {
		val, isExists := d.state[t1.Format(time.RFC822Z)]
		if isExists && val > 0 {
			return true
		}
		return false
	}
}

func DefaultTestScenario() DummyQueryProvider {
	return DummyQueryProvider{
		state: map[string]int{
			"02 Jan 06 15:04 +0000": 10,
			"02 Jan 06 15:05 +0000": 0,
			"02 Jan 06 15:06 +0000": 0,
			"02 Jan 06 15:07 +0000": 10,
			"02 Jan 06 15:08 +0000": 0,
			"02 Jan 06 15:09 +0000": 3,
			"02 Jan 06 15:10 +0000": 0,
			"02 Jan 06 15:11 +0000": 0,
			"02 Jan 06 15:12 +0000": 1,
		},
	}
}

func TestIteratorConstruct(t *testing.T) {
	scenario := DefaultTestScenario()
	iter := NewQueryIterator(scenario.StartTime(), scenario.EndTime(), true, scenario.QueryRunnerFunc(), scenario.HasDataFunc())

	currentWindow := iter.windows[0]
	if !(currentWindow.time == scenario.StartTime()) {
		t.Fatalf("window time does not match start, expected %s, actual %s", scenario.StartTime().String(), currentWindow.time.String())
	}
}

func TestIteratorAscending(t *testing.T) {
	scenario := DefaultTestScenario()
	iter := NewQueryIterator(scenario.StartTime(), scenario.EndTime(), true, scenario.QueryRunnerFunc(), scenario.HasDataFunc())

	iter.Next()
	// busy loop waiting for iter to be ready
	for !iter.Ready() {
		continue
	}

	currentWindow := iter.windows[iter.index]
	checkCurrentWindowIndex("02 Jan 06 15:04 +0000", currentWindow, t)

	// next should populate new window
	if iter.finished == true {
		t.Fatalf("Iter finished before expected")
	}
	if iter.ready == false {
		t.Fatalf("Iter is not ready when it should be")
	}

	iter.Next()
	// busy loop waiting for iter to be ready
	for !iter.Ready() {
		continue
	}

	currentWindow = iter.windows[iter.index]
	checkCurrentWindowIndex("02 Jan 06 15:07 +0000", currentWindow, t)

	iter.Next()
	// busy loop waiting for iter to be ready
	for !iter.Ready() {
		continue
	}

	currentWindow = iter.windows[iter.index]
	checkCurrentWindowIndex("02 Jan 06 15:09 +0000", currentWindow, t)

	iter.Next()
	// busy loop waiting for iter to be ready
	for !iter.Ready() {
		continue
	}

	currentWindow = iter.windows[iter.index]
	checkCurrentWindowIndex("02 Jan 06 15:12 +0000", currentWindow, t)

	if iter.finished != true {
		t.Fatalf("iter should be finished now but it is not")
	}
}

func TestIteratorDescending(t *testing.T) {
	scenario := DefaultTestScenario()
	iter := NewQueryIterator(scenario.StartTime(), scenario.EndTime(), false, scenario.QueryRunnerFunc(), scenario.HasDataFunc())

	iter.Next()
	// busy loop waiting for iter to be ready
	for !iter.Ready() {
		continue
	}

	currentWindow := iter.windows[iter.index]
	checkCurrentWindowIndex("02 Jan 06 15:12 +0000", currentWindow, t)

	// next should populate new window
	if iter.finished == true {
		t.Fatalf("Iter finished before expected")
	}
	if iter.ready == false {
		t.Fatalf("Iter is not ready when it should be")
	}

	iter.Next()
	// busy loop waiting for iter to be ready
	for !iter.Ready() {
		continue
	}

	currentWindow = iter.windows[iter.index]
	checkCurrentWindowIndex("02 Jan 06 15:09 +0000", currentWindow, t)

	iter.Next()
	// busy loop waiting for iter to be ready
	for !iter.Ready() {
		continue
	}

	currentWindow = iter.windows[iter.index]
	checkCurrentWindowIndex("02 Jan 06 15:07 +0000", currentWindow, t)

	iter.Next()
	// busy loop waiting for iter to be ready
	for !iter.Ready() {
		continue
	}

	currentWindow = iter.windows[iter.index]
	checkCurrentWindowIndex("02 Jan 06 15:04 +0000", currentWindow, t)

	if iter.finished != true {
		t.Fatalf("iter should be finished now but it is not")
	}
}

func checkCurrentWindowIndex(expectedValue string, currentWindow MinuteCheckPoint, t *testing.T) {
	expectedTime, _ := time.Parse(time.RFC822Z, expectedValue)
	if !(currentWindow.time == expectedTime) {
		t.Fatalf("window time does not match start, expected %s, actual %s", expectedTime.String(), currentWindow.time.String())
	}
}
