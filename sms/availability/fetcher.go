package availability

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

var debugAvail = os.Getenv("DEBUG_AVAILABILITY") == "true"

func availDebug(format string, args ...interface{}) {
	if debugAvail {
		fmt.Printf(format, args...)
	}
}

type AvailableSlot struct {
	Start    time.Time
	End      time.Time
	DayName  string
	DateStr  string
	TimeStr  string
	TZAbbrev string
}

type agentRef struct{ ID, Email, Name string }

var tzAbbreviations = map[string]string{
	"America/Phoenix":   "MST",
	"America/Anchorage":  "AKST",
	"Pacific/Honolulu":  "HST",
}

// getTZAbbrev returns the DST-aware timezone abbreviation for the given
// IANA timezone string at the specified time. Uses Go's native
// time.LoadLocation which correctly handles DST (e.g., "EDT" in summer,
// "EST" in winter for America/New_York). Falls back to the static map
// for timezones that cannot be loaded.
func getTZAbbrev(tz string, t time.Time) string {
	loc, err := time.LoadLocation(tz)
	if err == nil {
		abbr := t.In(loc).Format("MST")
		abbr = strings.Map(func(r rune) rune {
			if r >= 'A' && r <= 'Z' {
				return r
			}
			return -1
		}, abbr)
		if abbr != "" {
			return abbr
		}
	}
	if abbr, ok := tzAbbreviations[tz]; ok {
		return abbr
	}
	return tz
}

func FetchAvailableSlots(client *mongo.Client, propertyID, teamID string) []AvailableSlot {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var property bson.M
	err := client.Database("teams").Collection("properties").
		FindOne(ctx, bson.M{"id": propertyID}).Decode(&property)
	if err != nil {
		availDebug("[AVAIL-DEBUG] Property lookup FAILED for %s: %v\n", propertyID, err)
		return nil
	}
	pName, _ := property["propertyName"].(string)
	pStatus, _ := property["status"].(string)
	availDebug("[AVAIL-DEBUG] Step 1 ✅ Property: '%s' status='%s'\n", pName, pStatus)

	if status, _ := property["status"].(string); status == "off-market" {
		availDebug("[AVAIL-DEBUG] Step 1 ❌ Property is off-market\n")
		return nil
	}
	if v, ok := property["customScheduleUrl"].(string); ok && v != "" {
		availDebug("[AVAIL-DEBUG] Step 1 ❌ Property has customScheduleUrl='%s'\n", v)
		return nil
	}

	var assignedAgents []agentRef
	teamDoc := getTeamDoc(client, ctx, teamID)
	isCentralized := false
	if teamDoc != nil {
		if v, ok := teamDoc["centralized_mode_enabled"].(bool); ok {
			isCentralized = v
		}
	}
	availDebug("[AVAIL-DEBUG] Step 2: centralized=%v, units array type=%T\n", isCentralized, property["units"])

	if isCentralized {
		extractAgents(property["assignedAgents"], &assignedAgents)
		// Fallback: check units for assignedAgents
		if len(assignedAgents) == 0 {
			for _, um := range iterArr(property["units"]) {
				if extractAgents(um["assignedAgents"], &assignedAgents); len(assignedAgents) > 0 {
					break
				}
			}
		}
	} else {
		extractMember(property["assignedMember"], &assignedAgents)
		// Fallback: check units for assignedMember, then assignedAgents
		if len(assignedAgents) == 0 {
			for _, um := range iterArr(property["units"]) {
				extractMember(um["assignedMember"], &assignedAgents)
				if len(assignedAgents) == 0 {
					extractAgents(um["assignedAgents"], &assignedAgents)
				}
				if len(assignedAgents) > 0 {
					break
				}
			}
		}
	}

	unitCount := len(iterArr(property["units"]))
	availDebug("[AVAIL-DEBUG] Step 3: %d agents resolved (from root + %d units)\n", len(assignedAgents), unitCount)

	if len(assignedAgents) == 0 {
		availDebug("[AVAIL-DEBUG] Step 3 ❌ No agents found — returning nil\n")
		return nil
	}

	agentEmails := make([]string, 0, len(assignedAgents))
	for _, a := range assignedAgents {
		agentEmails = append(agentEmails, a.Email)
	}
	availDebug("[AVAIL-DEBUG] Step 3 ✅ Agents: %v\n", agentEmails)

	schedCursor, err := client.Database("Users").Collection("availability-schedules").
		Find(ctx, bson.M{"userEmail": bson.M{"$in": agentEmails}, "teamId": teamID})
	if err != nil {
		return nil
	}
	defer schedCursor.Close(ctx)

	var schedules []bson.M
	if err := schedCursor.All(ctx, &schedules); err != nil || len(schedules) == 0 {
		availDebug("[AVAIL-DEBUG] Step 4 ❌ No schedules found (err=%v, count=%d)\n", err, len(schedules))
		return nil
	}
	availDebug("[AVAIL-DEBUG] Step 4 ✅ Found %d schedule(s)\n", len(schedules))

	timezone := "America/Chicago"
	tourLength := 30
	bufferMinutes := 30

	if teamDoc != nil {
		if tz, ok := teamDoc["communication_timezone"].(string); ok && tz != "" {
			timezone = tz
		} else if tz, ok := teamDoc["team_timezone"].(string); ok && tz != "" {
			timezone = tz
		} else 		if tz, ok := teamDoc["centralized_timezone"].(string); ok && tz != "" {
			timezone = tz
		} else if len(schedules) > 0 {
			if tz, ok := schedules[0]["timezone"].(string); ok && tz != "" {
				timezone = tz
			}
		}
		if tl, ok := getIntVal(teamDoc["centralized_tour_length"]); ok {
			tourLength = tl
		}
		if bm, ok := getIntVal(teamDoc["centralized_buffer_minutes"]); ok {
			bufferMinutes = bm
		}
	} else if len(schedules) > 0 {
		if tz, ok := schedules[0]["timezone"].(string); ok && tz != "" {
			timezone = tz
		}
		if iv, ok := getIntVal(schedules[0]["interval"]); ok {
			tourLength = iv
		}
		if bm, ok := getIntVal(schedules[0]["bufferMinutes"]); ok {
			bufferMinutes = bm
		}
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	agentSched := schedules[0]
	workingDays := getStrSlice(agentSched["days"])
	globalHours := getHours(agentSched["hours"])

	dayHours := make(map[string]hEntry)
	if hbd, ok := agentSched["hoursByDay"].(bson.M); ok {
		for day, raw := range hbd {
			if m, ok := raw.(bson.M); ok {
				dayHours[strings.ToLower(day)] = getHours(m)
			}
		}
	}

	availDebug("[AVAIL-DEBUG] Step 5 — Settings: tz=%s tourLen=%d buf=%d interval=%d days=%v hours=%d:%02d-%d:%02d hoursByDay_keys=%d\n",
		timezone, tourLength, bufferMinutes, tourLength+bufferMinutes, workingDays,
		globalHours.startH, globalHours.startM, globalHours.endH, globalHours.endM, len(dayHours))

	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(8 * 24 * time.Hour)

	meetCursor, meetErr := client.Database("teams").Collection("meetings").
		Find(ctx, bson.M{
			"member.email": bson.M{"$in": agentEmails},
			"start":        bson.M{"$gte": dayStart.UTC(), "$lt": dayEnd.UTC()},
		})
	meetingSlots := make(map[string]bool)
	if meetErr == nil && meetCursor != nil {
		defer meetCursor.Close(ctx)
		var meetings []bson.M
		meetCursor.All(ctx, &meetings)
		for _, m := range meetings {
			start, sOK := m["start"].(time.Time)
			end, eOK := m["end"].(time.Time)
			if !sOK || !eOK {
				continue
			}
			bufStart := start.Add(-time.Duration(bufferMinutes) * time.Minute)
			bufEnd := end.Add(time.Duration(bufferMinutes) * time.Minute)
			for c := bufStart; c.Before(bufEnd); c = c.Add(time.Duration(tourLength) * time.Minute) {
				meetingSlots[c.In(loc).Format(time.RFC3339)] = true
			}
		}
	}

	blockedCursor, blockedErr := client.Database("Users").Collection("blocked-time-slots").
		Find(ctx, bson.M{
			"userEmail": bson.M{"$in": agentEmails},
			"start":     bson.M{"$lt": dayEnd.UTC()},
			"end":       bson.M{"$gt": dayStart.UTC()},
		})
	if blockedErr == nil && blockedCursor != nil {
		defer blockedCursor.Close(ctx)
		var blocked []bson.M
		blockedCursor.All(ctx, &blocked)
		for _, b := range blocked {
			start, sOK := b["start"].(time.Time)
			end, eOK := b["end"].(time.Time)
			if !sOK || !eOK {
				continue
			}
			for c := start; c.Before(end); c = c.Add(time.Duration(tourLength) * time.Minute) {
				meetingSlots[c.In(loc).Format(time.RFC3339)] = true
			}
		}
	}

	tasksCursor, tasksErr := client.Database("teams").Collection("tasks").
		Find(ctx, bson.M{
			"assignedMember.email": bson.M{"$in": agentEmails},
			"start":                bson.M{"$lt": dayEnd.UTC()},
			"end":                  bson.M{"$gt": dayStart.UTC()},
		})
	if tasksErr == nil && tasksCursor != nil {
		defer tasksCursor.Close(ctx)
		var tasks []bson.M
		tasksCursor.All(ctx, &tasks)
		for _, t := range tasks {
			start, sOK := t["start"].(time.Time)
			end, eOK := t["end"].(time.Time)
			if !sOK || !eOK {
				continue
			}
			for c := start; c.Before(end); c = c.Add(time.Duration(tourLength) * time.Minute) {
				meetingSlots[c.In(loc).Format(time.RFC3339)] = true
			}
		}
	}

	var allSlots []AvailableSlot
	effectiveInterval := tourLength + bufferMinutes

	for dayOffset := 0; dayOffset < 7; dayOffset++ {
		currentDay := dayStart.Add(time.Duration(dayOffset) * 24 * time.Hour)
		dayName := strings.ToLower(currentDay.Weekday().String())

		if len(workingDays) > 0 && !containsDay(workingDays, dayName) {
			continue
		}

		hours := globalHours
		if dh, ok := dayHours[dayName]; ok {
			hours = dh
		}
		if hours.startH == hours.endH {
			continue
		}

		slotStart := time.Date(currentDay.Year(), currentDay.Month(), currentDay.Day(), hours.startH, hours.startM, 0, 0, loc)
		dayEndTime := time.Date(currentDay.Year(), currentDay.Month(), currentDay.Day(), hours.endH, hours.endM, 0, 0, loc)

		for slotStart.Before(dayEndTime) {
			slotEnd := slotStart.Add(time.Duration(tourLength) * time.Minute)
			if slotEnd.After(dayEndTime) {
				break
			}
			if dayOffset == 0 && slotStart.Before(now.Add(time.Duration(bufferMinutes)*time.Minute)) {
				slotStart = slotStart.Add(time.Duration(effectiveInterval) * time.Minute)
				continue
			}

			slotKey := slotStart.In(loc).Format(time.RFC3339)
			if !meetingSlots[slotKey] {
				tzAbbr := getTZAbbrev(timezone, slotStart)
				allSlots = append(allSlots, AvailableSlot{
					Start:    slotStart.UTC(),
					End:      slotEnd.UTC(),
					DayName:  dayToDisplay(dayName),
					DateStr:  slotStart.Format("Jan 2"),
					TimeStr:  slotStart.Format("3:04 PM"),
					TZAbbrev: tzAbbr,
				})
			}
			slotStart = slotStart.Add(time.Duration(effectiveInterval) * time.Minute)
		}
	}

	sort.Slice(allSlots, func(i, j int) bool {
		return allSlots[i].Start.Before(allSlots[j].Start)
	})

	availDebug("[AVAIL-DEBUG] Step 9 — Total available slots across all days: %d\n", len(allSlots))

	// Group by day, cap at 3 per day so the AI can answer day-specific questions
	// ("what about Wednesday?") without blowing up the prompt with all 40 slots.
	type dayKey struct{ name, date string }
	dayCount := make(map[dayKey]int)
	var capped []AvailableSlot
	for _, s := range allSlots {
		k := dayKey{s.DayName, s.DateStr}
		if dayCount[k] >= 3 {
			continue
		}
		dayCount[k]++
		capped = append(capped, s)
	}
	allSlots = capped

	availDebug("[AVAIL-DEBUG] Step 10 — Returning top %d slots across %d days\n", len(allSlots), len(dayCount))
	for i, s := range allSlots {
		availDebug("[AVAIL-DEBUG]   Slot %d: %s, %s at %s (%s)\n", i+1, s.DayName, s.DateStr, s.TimeStr, s.TZAbbrev)
	}

	return allSlots
}

func FormatSlotsForPrompt(slots []AvailableSlot, propName string) string {
	if len(slots) == 0 {
		return "AVAILABLE TOUR TIMES: (none currently listed)"
	}
	var sb strings.Builder
	tz := "UTC"
	if len(slots) > 0 {
		tz = slots[0].TZAbbrev
	}
	if propName != "" {
		sb.WriteString(fmt.Sprintf("AVAILABLE TOUR TIMES for %s (in %s):\n", propName, tz))
	} else {
		sb.WriteString(fmt.Sprintf("AVAILABLE TOUR TIMES (in %s):\n", tz))
	}

	type dayGroup struct{ label string; times []string }
	var groups []dayGroup
	currentDay := ""
	for _, s := range slots {
		dayLabel := fmt.Sprintf("%s, %s", s.DayName, s.DateStr)
		if dayLabel == currentDay {
			groups[len(groups)-1].times = append(groups[len(groups)-1].times, s.TimeStr)
		} else {
			currentDay = dayLabel
			groups = append(groups, dayGroup{label: dayLabel, times: []string{s.TimeStr}})
		}
	}
	for _, g := range groups {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", g.label, strings.Join(g.times, ", ")))
	}
	return sb.String()
}

type hEntry struct{ startH, startM, endH, endM int }

func getHours(m interface{}) hEntry {
	bm, ok := m.(bson.M)
	if !ok {
		return hEntry{startH: 9, startM: 0, endH: 17, endM: 0}
	}
	startStr, _ := bm["start"].(string)
	endStr, _ := bm["end"].(string)
	if startStr == "" || endStr == "" {
		return hEntry{startH: 9, startM: 0, endH: 17, endM: 0}
	}
	var sh, sm, eh, em int
	fmt.Sscanf(startStr, "%d:%d", &sh, &sm)
	fmt.Sscanf(endStr, "%d:%d", &eh, &em)
	return hEntry{startH: sh, startM: sm, endH: eh, endM: em}
}

func getStrSlice(v interface{}) []string {
	switch arr := v.(type) {
	case bson.A:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return arr
	}
	return nil
}

func containsDay(days []string, day string) bool {
	for _, d := range days {
		if strings.ToLower(d) == day {
			return true
		}
	}
	return false
}

func dayToDisplay(day string) string {
	m := map[string]string{
		"monday": "Monday", "tuesday": "Tuesday", "wednesday": "Wednesday",
		"thursday": "Thursday", "friday": "Friday", "saturday": "Saturday", "sunday": "Sunday",
	}
	if d, ok := m[day]; ok {
		return d
	}
	return day
}

func getIntVal(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int: return n, true
	case int32: return int(n), true
	case int64: return int(n), true
	case float64: return int(n), true
	case float32: return int(n), true
	case string:
		var i int
		if _, err := fmt.Sscanf(n, "%d", &i); err == nil {
			return i, true
		}
	}
	return 0, false
}

func iterArr(v interface{}) []bson.M {
	switch arr := v.(type) {
	case primitive.A:
		result := make([]bson.M, 0, len(arr))
		for _, item := range arr {
			if m, ok := item.(bson.M); ok {
				result = append(result, m)
			}
		}
		return result
	}
	return nil
}

func extractAgents(v interface{}, agents *[]agentRef) bool {
	arr := iterArr(v)
	for _, m := range arr {
		email, _ := m["email"].(string)
		if email != "" {
			*agents = append(*agents, agentRef{Email: email})
		}
	}
	return len(*agents) > 0
}

func extractMember(v interface{}, agents *[]agentRef) {
	if m, ok := v.(bson.M); ok {
		email, _ := m["email"].(string)
		if email != "" {
			*agents = append(*agents, agentRef{Email: email})
		}
	}
}

func getTeamDoc(client *mongo.Client, ctx context.Context, teamID string) bson.M {
	var team bson.M
	client.Database("teams").Collection("teams").
		FindOne(ctx, bson.M{"teamId": teamID}).Decode(&team)
	return team
}
