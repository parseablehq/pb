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
	"time"
)

type MinuteCheckPoint struct {
	// minute start time.
	time time.Time
}

type QueryIterator[OK any, ERR any] struct {
	rangeStartTime time.Time
	rangeEndTime   time.Time
	ascending      bool
	index          int
	windows        []MinuteCheckPoint
	ready          bool
	finished       bool
	queryRunner    func(time.Time, time.Time) (OK, ERR)
	hasData        func(time.Time, time.Time) bool
}

func NewQueryIterator[OK any, ERR any](startTime time.Time, endTime time.Time, ascending bool, queryRunner func(time.Time, time.Time) (OK, ERR), hasData func(time.Time, time.Time) bool) QueryIterator[OK, ERR] {
	iter := QueryIterator[OK, ERR]{
		rangeStartTime: startTime,
		rangeEndTime:   endTime,
		ascending:      ascending,
		index:          -1,
		windows:        []MinuteCheckPoint{},
		ready:          true,
		finished:       false,
		queryRunner:    queryRunner,
		hasData:        hasData,
	}
	iter.populateNextNonEmpty()
	return iter
}

func (iter *QueryIterator[OK, ERR]) inRange(targetTime time.Time) bool {
	return targetTime.Equal(iter.rangeStartTime) || (targetTime.After(iter.rangeStartTime) && targetTime.Before(iter.rangeEndTime))
}

func (iter *QueryIterator[OK, ERR]) Ready() bool {
	return iter.ready
}

func (iter *QueryIterator[OK, ERR]) Finished() bool {
	return iter.finished && iter.index == len(iter.windows)-1
}

func (iter *QueryIterator[OK, ERR]) CanFetchPrev() bool {
	return iter.index > 0
}

func (iter *QueryIterator[OK, ERR]) populateNextNonEmpty() {
	var inspectMinute MinuteCheckPoint

	// this is initial condition when no checkpoint exists in the window
	if len(iter.windows) == 0 {
		if iter.ascending {
			inspectMinute = MinuteCheckPoint{time: iter.rangeStartTime}
		} else {
			inspectMinute = MinuteCheckPoint{iter.rangeEndTime.Add(-time.Minute)}
		}
	} else {
		inspectMinute = MinuteCheckPoint{time: nextMinute(iter.windows[len(iter.windows)-1].time, iter.ascending)}
	}

	iter.ready = false
	for iter.inRange(inspectMinute.time) {
		if iter.hasData(inspectMinute.time, inspectMinute.time.Add(time.Minute)) {
			iter.windows = append(iter.windows, inspectMinute)
			iter.ready = true
			return
		}
		inspectMinute = MinuteCheckPoint{
			time: nextMinute(inspectMinute.time, iter.ascending),
		}
	}

	// if the loops breaks we have crossed the range with no data
	iter.ready = true
	iter.finished = true
}

func (iter *QueryIterator[OK, ERR]) Next() (OK, ERR) {
	// This assumes that there is always a next index to fetch if this function is called
	iter.index++
	currentMinute := iter.windows[iter.index]
	if iter.index == len(iter.windows)-1 {
		iter.ready = false
		go iter.populateNextNonEmpty()
	}
	return iter.queryRunner(currentMinute.time, currentMinute.time.Add(time.Minute))
}

func (iter *QueryIterator[OK, ERR]) Prev() (OK, ERR) {
	if iter.index > 0 {
		iter.index--
	}
	currentMinute := iter.windows[iter.index]
	return iter.queryRunner(currentMinute.time, currentMinute.time.Add(time.Minute))
}

func nextMinute(current time.Time, ascending bool) time.Time {
	if ascending {
		return current.Add(time.Minute)
	}
	return current.Add(-time.Minute)
}
