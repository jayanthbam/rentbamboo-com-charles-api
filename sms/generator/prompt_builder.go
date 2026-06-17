package generator

import (
	"fmt"
	"strings"
	"time"

	"bamboo/types"
)

// PromptConfig holds all pre-computed data needed to build the system prompt.
// The caller assembles this struct; BuildSystemPrompt just renders it.
type PromptConfig struct {
	// Identity
	SigningName string

	// Qualification
	Requirements string   // from priorities + keyInfo (extracted)
	KeyQuestions []string // max 2 most relevant
	Personality  string
	Highlights   string

	// Feature flags + data
	AppSending      bool
	AppURL          string
	TourScheduling  bool
	ScheduleURL     string
	AvailabilityCtx string // pre-formatted slots, empty if none
	AppNeeds        string // application requirements text

	// Property
	PropertyCtx  string // pre-built property text
	PropertyName string

	// Context
	TeamName        string
	TeamDescription string
	TeamCity        string
	TeamState       string
	TeamPhone       string
	TeamWebsite     string
	TeamDomain      string
	LeadFirstName   string
	LeadLastName    string
	LeadEmail       string
	LeadPhone       string
	LeadBudget      string
	LeadMoveIn      string
	LeadJobTitle    string
	LeadIndustry    string
	LeadSource      string
	LeadStatus      string
	LeadComments    []string
	LeadTags        []string

	// New parsed fields (from AI lead parser)
	LeadBedroomPreference string
	LeadPets              string

	// Separate sections
	KeyInfo    string // from command center keyInfo
	Priorities string // from command center priorities

	// Dynamic state (computed per-request from chat history)
	CompletedItems string
	QualStatus     string
	LatestMessage  string // The lead's most recent message — surfaced at the top of the prompt

	// Structured qualification (Phase 1) — replaces CompletedItems/QualStatus when mode=structured
	QualificationCtx      QualificationContext
	QualifyWithoutProperty bool // When true + no property: AI qualifies without discussing property

	// HumanTakeover is true when the most recent outbound message in
	// the chat history was sent by a human agent (SentBy != ""). When
	// true, the prompt builder injects a "HUMAN TAKEOVER — HIGH PRIORITY"
	// section at the very top, before STRICT_RULES, instructing the AI
	// to match the human's tone and not contradict or repeat their
	// message. This is the strongest signal in the prompt.
	HumanTakeover bool

	// IsFullyQualified is true when all of the lead's qualification
	// questions have been answered (AnsweredAt != nil). When true, the
	// prompt builder renders a "✓ LEAD IS FULLY QUALIFIED" section,
	// swaps the goal hierarchy + target wording to "PUSH for tour /
	// close-out" mode, and drops the ASK_NEXT importance line.
	IsFullyQualified bool

	// ToursScheduled is formatted text describing the lead's upcoming
	// tours (from the meetings collection, status="scheduled" with
	// start > now). Rendered in the TOUR HISTORY section.
	ToursScheduled string

	// ToursPrevious is formatted text describing the lead's past tours
	// (status="scheduled" with start < now, or status="cancelled",
	// "no-show", "completed", etc.). Rendered in the TOUR HISTORY section.
	ToursPrevious string

	// Private team-internal notes about the lead. NEVER exposed to the
	// lead. Rendered in the PRIVATE TEAM NOTES section. Use this to
	// give the AI context about prior conversations, complaints, or
	// special situations the team wants the AI to be aware of.
	LeadNotes []types.LeadNote

	// CurrentDateTime is the timestamp used to render CURRENT_DATE /
	// CURRENT_TIME in MISCELLANEOUS. If zero, the AI prompt won't
	// include current date/time.
	CurrentDateTime time.Time

	// TeamTimezone is the IANA timezone string (e.g. "America/Chicago")
	// used to render CURRENT_TIME in the team's local timezone. Empty
	// → no timezone line in MISCELLANEOUS.
	TeamTimezone string

	// Notes
	PropertySwitchNote string
	TrainingCtx        string // SMS training examples, pre-built
}

// BuildSystemPrompt constructs a single system message from the config.
func BuildSystemPrompt(cfg PromptConfig) string {
	var b strings.Builder

	// ─ Conditional goal hierarchy + target wording ──
	// When the lead is fully qualified, swap to "PUSH for tour / close-out"
	// mode. Otherwise, default to "qualify first" mode.
	goalHierarchy := "qualify first, push for a tour once qualified or showing interest, and only mention applications after a tour is scheduled or when specifically asked."
	yourTarget := "First qualify the lead. If all questions are answered, reply based on the importance order below."
	importanceLine6 := "6. ASK_NEXT (unanswered — pick 1-2 to weave into your reply when needed)"
	if cfg.IsFullyQualified {
		// Sub-conditions within fully-qualified:
		//   - Tour already scheduled → "be of help" mode (no pushing)
		//   - No tour scheduled → "push for tour / close-out" mode
		if cfg.ToursScheduled != "" {
			goalHierarchy = "BE OF HELP — qualified AND a tour is already scheduled. Do NOT push for a new tour. Do NOT re-ask answered questions (unless you sense a property change — stale link is not a property change, look at other traces in the previous conversations). Only mention applications after a tour is scheduled or when specifically asked."
			yourTarget = "Just be of property help. Tour is already scheduled (see TOUR HISTORY below). Answer questions about the property, confirm the tour time if asked, offer to help with anything else. Be concise — the lead has been patient."
			importanceLine6 = "6. (no unanswered questions — tour already scheduled, just be of help)"
		} else {
			goalHierarchy = "PUSH for a tour (or close-out) — all qualification questions answered. Don't re-ask anything. Only mention applications after a tour is scheduled or when specifically asked."
			yourTarget = "Push for a tour or close-out. All qualification questions answered. Be concise — the lead has been patient."
			importanceLine6 = "6. (no unanswered questions — focus on PUSH for tour / close-out)"
		}
	}

	// ─ Opening ────────────────────────────────────────────────────────────
	fmt.Fprintf(&b, `You are %s, a leasing agent from a team. Reply to the lead whose Latest Message is: %q.

You are an SMS-only leasing agent: plain text (no markdown/HTML), max 400 characters per message, no legal disclaimers or "Reply STOP", write like a human texting, always end with one clear next step. You're an AI agent (not a person) — if the lead wants to talk to a human or asks for a phone number, say the team will follow up via the contact phone in PROPERTY_CONTEXT (not "me" or "I"). 

	If you see messages labeled "Team:" in the chat history, those were sent by a human agent (not you) — match their tone and don't contradict or repeat what they said. ** THIS precedes over the AI. **

Timestamps in the chat history show when each message was sent. Use them to gauge recency: if the lead was silent for hours/days you can acknowledge the gap or be more concise; if they're actively engaged, match the energy. DON'T MAKE IT FEEL LIKE A NEW SMS. MAKE IT FEEL LIKE A CONTINUATION to their Latest Text.

Discuss only the property in context below — use "property" over "apartment", copy prices character-for-character, list only units explicitly listed, never invent info, and say yes if asked if you're AI. Goal hierarchy: %s

Your target: %s

Importance order (0 = first):
0. STRICT_RULES
0.5. PERSONALITY
1. LEAD_CONTEXT
2. PROPERTY_CONTEXT
3. CAPABILITIES
4. KEY_INFO
5. INFORMATION_CONTEXT (already answered — for reference only)
%s
7. PRIORITIES
8. HIGHLIGHTS
9. TEAM_TRAINING
10. MISCELLANEOUS

`, cfg.SigningName, cfg.LatestMessage, goalHierarchy, yourTarget, importanceLine6)

	// ─ HUMAN TAKEOVER (HIGH PRIORITY) — injected when most recent ──────
	// outbound was sent by a human agent. Placed at the very top of the
	// system prompt, before STRICT_RULES, so the AI gives it the highest
	// attention. Only rendered when cfg.HumanTakeover == true.
	if cfg.HumanTakeover {
		b.WriteString(`═════════════
🔴 HUMAN TAKEOVER — HIGH PRIORITY
═════════════
A team member (NOT the AI) sent the most recent outbound message(s) in the chat history. Their message is the latest context the lead has received. You MUST:
1. Match the human agent's tone and style exactly.
2. NOT contradict what they said (no correcting prices, dates, property details).
3. NOT repeat what they already said (the lead has already seen it).
4. Build on top of their message — add value, not redundancy.

**THIS PRECEDES OVER THE AI. ** Treat their message as the current state of the conversation.

`)
	}

	// ─ FULLY QUALIFIED marker (only when all questions are answered) ──
	// Strong signal to the AI that the conversation is in "PUSH for tour"
	// mode, not "qualify" mode. Placed high in the prompt so it has
	// strong attention.
	if cfg.IsFullyQualified {
		if cfg.ToursScheduled != "" {
			// Sub-case A: tour already scheduled
			b.WriteString(`═════════════
✓ LEAD IS FULLY QUALIFIED (tour already scheduled)
═════════════
All qualification questions answered. Tour is already scheduled — see TOUR HISTORY below.
- Do NOT re-ask any of the answered questions, unless you sense property change. Stale link is not property change, look at other traces in the previous conversations.
- Do NOT push for a new tour. The lead is already booked.
- Just be of property help — answer questions, confirm the tour time if asked, offer to help with anything else.
- Be concise — the lead has been patient.

`)
		} else {
			// Sub-case B: no tour yet
			b.WriteString(`═════════════
✓ LEAD IS FULLY QUALIFIED (no tour scheduled)
═════════════
All qualification questions answered. Profile fields collected.
- Do NOT re-ask any of the answered questions, unless you sense property change. Stale link is not property change, look at other traces in the previous conversations.
- Move to PUSH: tour scheduling or close-out.
- Be concise — the lead has been patient.

`)
		}
	}

	// ─ Section 0.25: NEXT TOUR callout (only when lead has an
	// upcoming tour scheduled) ──
	// Placed high in the prompt so the AI always sees it. Mirrors
	// the SCHEDULED sub-section of TOUR HISTORY but as a one-line
	// callout, so the AI never misses the lead's existing booking.
	if cfg.ToursScheduled != "" {
		b.WriteString(`═════════════
📅 NEXT TOUR (lead has an upcoming tour)
═════════════
The lead has at least one tour scheduled. Times are in the team's local timezone.
`)
		b.WriteString(cfg.ToursScheduled)
		b.WriteString("\n")
	}

	// ─ Section 0.5: PERSONALITY (only when set) ──────────────────────────
	// Renders the team's chosen communication style. Maps the 5 Zod enum
	// values (consultant/challenger/relationship/expert/closer) to a
	// short tone guide. Empty personality → no section rendered.
	if cfg.Personality != "" {
		style, ok := personalityStyle(cfg.Personality)
		if ok {
			b.WriteString(fmt.Sprintf(`═════════════
0.5. PERSONALITY (your texting style)
═════════════
%s

`, style))
		}
	}

	// ─ Section 0: STRICT_RULES ────────────────────────────────────────────
	b.WriteString(`═════════════
0. STRICT_RULES
═════════════
1. Facts from LEAD_CONTEXT and PROPERTY_CONTEXT only. Old convos may be stale.
2. If a question should be asked, you can ask up to 2 at once.
3. Greetings -> greet back. No pitch, no questions.
4. Lead wants to tour -> Check CAPABILITIES.
5. Lead asks to apply -> Check CAPABILITIES.
6. Never repeat unless lead asks again. Past conversation may have STALE URLs, prices, or property names — use LEAD_CONTEXT and PROPERTY_CONTEXT for current truth, not old messages.
7. Not in LEAD or PROPERTY context? -> "Not available."
8. If you've asked the same question 2x with no relevant answer, skip it and move to the next unanswered.
9. PRIVATE sections (e.g. PRIVATE TEAM NOTES) are for YOUR context only. You must NEVER quote, paraphrase, summarize, or hint at their content to the lead. Treat their content as if it doesn't exist when crafting your reply.
`)

	// Conditional rule 10: only rendered when tour scheduling is ON and we
	// have a tour link to redirect to. When tour sending is OFF, this
	// rule doesn't render — the AI has no link to point to. TEAM_TRAINING
	// (section 9 in the importance list) can override this rule with
	// specific links (Instagram, YouTube, virtual tour, etc.) if the
	// team has them.
	if cfg.TourScheduling && cfg.ScheduleURL != "" {
		b.WriteString(`10. If the lead asks for a photo, video, floor plan, virtual tour, or any visual of the property -> redirect them to the TOUR_LINK and tell them photos/floor plan are available there. Do NOT describe the property visually in text. Just point them to the tour link.

11. Don't spam the tour link. While qualifying (not yet fully qualified), mention the TOUR_LINK at most once or twice across the whole conversation — NOT on every reply. After the lead has already seen the link (or has a tour scheduled — see TOUR HISTORY above), do NOT re-push the tour link unless the lead explicitly asks about touring or you have new information (e.g. a new unit became available). The lead's patience is more important than a booking. `)
	}

	// ─ Section 1: LEAD_CONTEXT ────────────────────────────────────────────
	var leadLines []string
	hasLeadInfo := cfg.LeadFirstName != "" || cfg.LeadLastName != "" || cfg.LeadStatus != "" ||
		cfg.LeadBudget != "" || cfg.LeadMoveIn != "" || cfg.LeadJobTitle != "" ||
		cfg.LeadBedroomPreference != "" || cfg.LeadPets != "" ||
		cfg.LeadEmail != "" || cfg.LeadPhone != "" || cfg.LeadSource != "" ||
		cfg.LeadIndustry != "" || len(cfg.LeadTags) > 0 || len(cfg.LeadComments) > 0
	if hasLeadInfo {
		b.WriteString("═════════════\n1. LEAD_CONTEXT (lead info — only source of truth)\n═════════════\n")
		if cfg.LeadFirstName != "" || cfg.LeadLastName != "" {
			fmt.Fprintf(&b, "Name: %s %s\n", cfg.LeadFirstName, cfg.LeadLastName)
		}
		if cfg.LeadEmail != "" {
			fmt.Fprintf(&b, "Email: %s\n", cfg.LeadEmail)
		}
		if cfg.LeadPhone != "" {
			fmt.Fprintf(&b, "Phone: %s\n", cfg.LeadPhone)
		}
		if cfg.LeadStatus != "" {
			fmt.Fprintf(&b, "Status: %s\n", cfg.LeadStatus)
		}
		if cfg.LeadSource != "" {
			fmt.Fprintf(&b, "Source: %s\n", cfg.LeadSource)
		}
		if len(cfg.LeadTags) > 0 {
			fmt.Fprintf(&b, "Tags: [%s]\n", strings.Join(cfg.LeadTags, ", "))
		}
		if cfg.LeadBudget != "" {
			fmt.Fprintf(&b, "Budget: %s\n", cfg.LeadBudget)
		}
		if cfg.LeadMoveIn != "" {
			fmt.Fprintf(&b, "Move-In: %s\n", cfg.LeadMoveIn)
		}
		if cfg.LeadJobTitle != "" {
			fmt.Fprintf(&b, "Job Title: %s\n", cfg.LeadJobTitle)
		}
		if cfg.LeadIndustry != "" {
			fmt.Fprintf(&b, "Industry: %s\n", cfg.LeadIndustry)
		}
		if cfg.LeadPets != "" {
			fmt.Fprintf(&b, "Pets: %s\n", cfg.LeadPets)
		}
		if cfg.LeadBedroomPreference != "" {
			fmt.Fprintf(&b, "Bedroom Pref: %s\n", cfg.LeadBedroomPreference)
		}
		if len(cfg.LeadComments) > 0 {
			fmt.Fprintf(&b, "Comments: %s\n", strings.Join(cfg.LeadComments, "; "))
		}
		b.WriteString("\n")
	}
	_ = leadLines // reserved

	// ─ Section 1.25: PRIVATE TEAM NOTES (only when notes exist) ──
	// Private context the team has recorded about this lead. NEVER
	// expose, quote, or hint at the content of these notes to the
	// lead. The STRICT_RULE for PRIVATE sections reinforces this.
	if len(cfg.LeadNotes) > 0 {
		b.WriteString("═════════════\n1.25. PRIVATE TEAM NOTES (NEVER EXPOSE TO LEAD — internal context only)\n═════════════\n")
		for _, n := range cfg.LeadNotes {
			ts := n.CreatedAt
			if !ts.IsZero() {
				fmt.Fprintf(&b, "- [%s] %s: %s\n", ts.Format("2006-01-02"), n.AuthorName, n.Content)
			} else {
				fmt.Fprintf(&b, "- %s: %s\n", n.AuthorName, n.Content)
			}
		}
		b.WriteString("\n")
	}

	// ─ Section 1.5: TOUR HISTORY (only when tours exist) ──
	// Shows the lead's tour history (upcoming + past) so the AI knows
	// not to push for a new tour if one is already scheduled, and to
	// follow up appropriately on previous tours.
	if cfg.ToursScheduled != "" || cfg.ToursPrevious != "" {
		b.WriteString("═════════════\n1.5. TOUR HISTORY\n═════════════\n")
		if cfg.ToursScheduled != "" {
			b.WriteString("SCHEDULED:\n")
			b.WriteString(cfg.ToursScheduled)
			b.WriteString("\n")
		}
		if cfg.ToursPrevious != "" {
			b.WriteString("PREVIOUS:\n")
			b.WriteString(cfg.ToursPrevious)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// ─ Section 2: PROPERTY_CONTEXT ────────────────────────────────────────
	noProperty := cfg.PropertyCtx == "" && cfg.PropertyName == ""
	if noProperty && cfg.QualifyWithoutProperty {
		b.WriteString("═════════════\n2. PROPERTY_CONTEXT\n═════════════\n")
		b.WriteString("No property assigned. You can ask qualification questions.\n")
		b.WriteString("You have no properties to discuss rent, pricing, units, or amenities.\n")
		b.WriteString("Once qualified fully, say an agent will help them find the right property.\n\n")
	} else if noProperty {
		b.WriteString("═════════════\n2. PROPERTY_CONTEXT\n═════════════\n")
		b.WriteString("No property assigned. Ask which property they mean.\n\n")
	} else {
		b.WriteString("═════════════\n2. PROPERTY_CONTEXT (property info — only source of truth)\n═════════════\n")
		if cfg.PropertySwitchNote != "" {
			fmt.Fprintf(&b, "⚠️ PROPERTY SWITCH: %s\n", cfg.PropertySwitchNote)
		}
		b.WriteString(cfg.PropertyCtx)
		b.WriteString("\n")
		if cfg.AvailabilityCtx != "" {
			b.WriteString("\nTOUR_TIMES:\n")
			b.WriteString(cfg.AvailabilityCtx)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// ─ Section 3: CAPABILITIES ────────────────────────────────────────────
	b.WriteString("═════════════\n3. CAPABILITIES\n═════════════\n")
	hasTourLink := cfg.TourScheduling && cfg.ScheduleURL != ""
	hasAppLink := cfg.AppSending && cfg.AppURL != ""

	if hasTourLink {
		b.WriteString("- TOUR_SENDING: ON — use the TOUR_TIMES + TOUR_LINK for scheduling.\n")
	} else {
		b.WriteString("- TOUR_SENDING: OFF — Do NOT mention touring or scheduling. STRICTLY deny if asked or do not mention according to the lead's latest sms.\n")
	}
	if hasAppLink {
		b.WriteString("- APPLICATION_SENDING: ON — send APPLICATION_LINK when asked or you feel is the right time.\n")
	} else {
		b.WriteString("- APPLICATION_SENDING: OFF — Do not mention Applications, STRICTLY deny if asked or do not mention according to the lead's latest sms.\n")
	}
	if !hasTourLink && !hasAppLink {
		b.WriteString("- ** YOU HAVE NO LINKS TO SHARE. NEVER reference old conversations for STALE URLS, or NEVER makeup URLS or guess URLS. **\n")
	}
	b.WriteString("\n")

	// ─ Section 4: KEY_INFO ────────────────────────────────────────────────
	if cfg.KeyInfo != "" {
		fmt.Fprintf(&b, "═════════════\n4. KEY_INFO (From the team)\n═════════════\n%s\n\n", cfg.KeyInfo)
	}

	// ─ Sections 5-6: INFORMATION_CONTEXT + INFORMATION_NEEDED ────────────
	if cfg.QualificationCtx.Mode == "structured" && len(cfg.QualificationCtx.Questions) > 0 {
		var answered, unanswered []string
		for _, q := range cfg.QualificationCtx.Questions {
			switch q.Status {
			case "answered":
				answered = append(answered, fmt.Sprintf("- %q -> %q", q.Question, q.Answer))
			case "unanswered":
				unanswered = append(unanswered, fmt.Sprintf("- %q", q.Question))
			case "not-asked":
				unanswered = append(unanswered, fmt.Sprintf("- %q", q.Question))
			}
		}
		if len(answered) > 0 {
			fmt.Fprintf(&b, "═════════════\n5. INFORMATION_CONTEXT (already answered, just for reference)\n═════════════\n%s\n\n", strings.Join(answered, "\n"))
		}
		if len(unanswered) > 0 {
			var suffix string
			switch {
			case cfg.TourScheduling && cfg.AppSending:
				suffix = ", even when also pushing tour/application"
			case cfg.TourScheduling:
				suffix = ", even when also pushing tour"
			case cfg.AppSending:
				suffix = ", even when also pushing application"
			}
			fmt.Fprintf(&b, "═════════════\n6. ASK_NEXT (unanswered — pick 1-2 to weave INTO your reply%s)\n═════════════\n%s\n\n", suffix, strings.Join(unanswered, "\n"))
		}
	}

	// ─ Section 7: PRIORITIES ──────────────────────────────────────────────
	if cfg.Priorities != "" {
		fmt.Fprintf(&b, "═════════════\n7. PRIORITIES (team's priorities they need you to say or ask,  or check the conversation history if already told then it is fine)\n═════════════\n%s\n\n", cfg.Priorities)
	}

	// ─ Section 8: HIGHLIGHTS ──────────────────────────────────────────────
	if cfg.Highlights != "" {
		fmt.Fprintf(&b, "═════════════\n8. HIGHLIGHTS (if relevant to context)\n═════════════\n%s\n\n", cfg.Highlights)
	}

	// ─ Section 9: TEAM_TRAINING ──────────────────────────────────────────
	if cfg.TrainingCtx != "" {
		fmt.Fprintf(&b, "═════════════\n9. TEAM_TRAINING (overrides rules above if they conflict)\n═════════════\n%s\n\n", cfg.TrainingCtx)
	}

	// ─ Section 10: MISCELLANEOUS ──────────────────────────────────────────
	hasTeamInfo := cfg.TeamName != "" || cfg.TeamDescription != "" || cfg.TeamCity != "" || cfg.TeamState != ""
	hasTimeInfo := !cfg.CurrentDateTime.IsZero() || cfg.TeamTimezone != ""
	if hasTeamInfo || hasTimeInfo {
		b.WriteString("═════════════\n10. MISCELLANEOUS\n═════════════\n")
		if cfg.TeamName != "" {
			fmt.Fprintf(&b, "TEAM_NAME: %s\n", cfg.TeamName)
		}
		if cfg.TeamCity != "" || cfg.TeamState != "" {
			fmt.Fprintf(&b, "TEAM_BASED: %s, %s\n", cfg.TeamCity, cfg.TeamState)
		}
		if cfg.TeamDescription != "" {
			fmt.Fprintf(&b, "TEAM_DESCRIPTION: %s\n", cfg.TeamDescription)
		}
		if !cfg.CurrentDateTime.IsZero() {
			loc, _ := time.LoadLocation(cfg.TeamTimezone)
			if loc == nil {
				loc = time.UTC
			}
			localTime := cfg.CurrentDateTime.In(loc)
			fmt.Fprintf(&b, "CURRENT_DATE: %s (%s)\n", localTime.Format("2006-01-02"), localTime.Weekday().String())
			fmt.Fprintf(&b, "CURRENT_TIME: %s\n", localTime.Format("3:04 PM MST"))
		}
		if cfg.TeamTimezone != "" {
			fmt.Fprintf(&b, "TEAM_TIMEZONE: %s\n", cfg.TeamTimezone)
		}
		b.WriteString("\n")
	}

	b.WriteString("FINAL: Getting them through the door is more important than answering every question.\n")

	if cfg.TourScheduling {
		b.WriteString("PUSH BALANCE: push for a tour AND weave 1-2 questions from ASK_NEXT into the same message. ONLY DO IT IF IT MAKES SENSE NATURALLY. \n")
	}
	if cfg.AppSending {
		b.WriteString("PUSH BALANCE: push for an application AND weave 1-2 questions from ASK_NEXT into the same message. ONLY DO IT IF IT MAKES SENSE NATURALLY.\n")
	}

	return b.String()
}

// personalityStyle maps the command-center personality enum to a short tone
// guide rendered in the system prompt. Returns the style text and a bool
// indicating whether the personality value was recognized. Unknown values
// fall back to a sensible default.
func personalityStyle(p string) (string, bool) {
	switch p {
	case "relationship":
		return "You communicate in a Relationship Builder style: professional, warm, friendly, build rapport first, remember details from past messages, light emojis OK, never pushy.", true
	case "consultant":
		return "You communicate in a Consultative style: professional, ask thoughtful diagnostic questions, present options with pros/cons, collaborative tone.", true
	case "challenger":
		return "You communicate in a Challenger style: professional, push back on objections, ask probing questions, challenge assumptions, direct and provocative.", true
	case "expert":
		return "You communicate in an Expert style: authoritative, data-driven, cite specific prices/amenities, lead with expertise.", true
	case "closer":
		return "You communicate in a Closer style: action-oriented, create urgency, assume the sale, focus on next step (tour/application based on capabilities).", true
	default:
		return "You communicate in a professional, warm, and direct style — a leasing agent texting a prospective renter.", true
	}
}
