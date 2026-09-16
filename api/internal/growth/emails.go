package growth

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/buildingvision/api/internal/platform/mailer"
)

// Email copy mengikuti Website PRD §32, §37, §38: bahasa Inggris, manusiawi, tanpa em dash.

func layout(title, intro string, paragraphs []string, ctaLabel, ctaURL, footer string) (text, htmlBody string) {
	var t strings.Builder
	t.WriteString(title + "\n\n")
	if intro != "" {
		t.WriteString(intro + "\n\n")
	}
	for _, p := range paragraphs {
		t.WriteString(p + "\n\n")
	}
	if ctaURL != "" {
		t.WriteString(ctaLabel + ": " + ctaURL + "\n\n")
	}
	if footer != "" {
		t.WriteString(footer + "\n")
	}
	var h strings.Builder
	h.WriteString(`<div style="font-family:Inter,Segoe UI,Arial,sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#111827;line-height:1.55">`)
	h.WriteString(`<div style="font-weight:700;font-size:18px;color:#0B776F;margin-bottom:24px">BuildingVision</div>`)
	h.WriteString(`<h1 style="font-size:22px;margin:0 0 12px">` + html.EscapeString(title) + `</h1>`)
	if intro != "" {
		h.WriteString(`<p style="margin:0 0 16px;font-size:16px">` + html.EscapeString(intro) + `</p>`)
	}
	for _, p := range paragraphs {
		h.WriteString(`<p style="margin:0 0 16px;font-size:15px;color:#374151">` + html.EscapeString(p) + `</p>`)
	}
	if ctaURL != "" {
		h.WriteString(`<p style="margin:24px 0"><a href="` + html.EscapeString(ctaURL) + `" style="background:#0B776F;color:#fff;text-decoration:none;padding:12px 20px;border-radius:10px;font-weight:600;display:inline-block">` + html.EscapeString(ctaLabel) + `</a></p>`)
		h.WriteString(`<p style="font-size:13px;color:#6B7280">If the button does not work, copy this link into your browser:<br>` + html.EscapeString(ctaURL) + `</p>`)
	}
	if footer != "" {
		h.WriteString(`<p style="font-size:13px;color:#6B7280;margin-top:32px">` + html.EscapeString(footer) + `</p>`)
	}
	h.WriteString(`</div>`)
	return t.String(), h.String()
}

func firstName(full string) string {
	full = strings.TrimSpace(full)
	if i := strings.IndexByte(full, ' '); i > 0 {
		return full[:i]
	}
	if full == "" {
		return "there"
	}
	return full
}

func verifyEmail(name, link string, ttl time.Duration) mailer.Message {
	hours := int(ttl.Hours())
	text, htm := layout(
		"Confirm your email to start your free trial",
		fmt.Sprintf("Hi %s, thanks for signing up for BuildingVision.", firstName(name)),
		[]string{
			"Confirm your email address and we will set up your trial workspace right away. No credit card is needed.",
			fmt.Sprintf("This link is valid for %d hours.", hours),
		},
		"Confirm email", link,
		"If you did not create a BuildingVision account, you can safely ignore this email.")
	return mailer.Message{Subject: "Confirm your email for BuildingVision", Text: text, HTML: htm}
}

func welcomeEmail(name, org, appURL string) mailer.Message {
	text, htm := layout(
		"Welcome to BuildingVision",
		fmt.Sprintf("Hi %s, your workspace for %s is ready.", firstName(name), org),
		[]string{
			"BuildingVision brings housekeeping, engineering, security, and tenant relation together in one place, so your team always knows what needs attention today.",
			"Next step: choose the profile that fits your property (Hotel, Apartment, or Office) and create your first property. It takes about two minutes.",
		},
		"Open your dashboard", appURL+"/onboarding",
		"Need a hand? Reply to this email and a real person will get back to you.")
	return mailer.Message{Subject: "Welcome to BuildingVision", Text: text, HTML: htm}
}

func trialStartedEmail(name, org, appURL string, ends time.Time, days int) mailer.Message {
	text, htm := layout(
		"Your free trial has started",
		fmt.Sprintf("Hi %s, your %d-day trial for %s runs until %s.", firstName(name), days, org, ends.Format("2 January 2006")),
		[]string{
			"Everything is unlocked during the trial. To see the real value quickly, try this: create a request, turn it into a work order, complete it, and attach a photo as evidence.",
			"You can also load a sample property to explore the product without entering data by hand.",
		},
		"Complete your setup", appURL+"/onboarding",
		"")
	return mailer.Message{Subject: "Your BuildingVision trial has started", Text: text, HTML: htm}
}

func setupReminderEmail(name, org, appURL string) mailer.Message {
	text, htm := layout(
		"Complete your setup",
		fmt.Sprintf("Hi %s, %s is almost ready to go.", firstName(name), org),
		[]string{
			"A few short steps are left on your onboarding checklist: add your building areas, invite your team, and run your first work order end to end.",
		},
		"Continue setup", appURL+"/onboarding",
		"")
	return mailer.Message{Subject: "Complete your BuildingVision setup", Text: text, HTML: htm}
}

func trialEndingSoonEmail(name, org, appURL string, ends time.Time) mailer.Message {
	daysLeft := int(time.Until(ends).Hours()/24) + 1
	if daysLeft < 1 {
		daysLeft = 1
	}
	text, htm := layout(
		"Your trial is ending soon",
		fmt.Sprintf("Hi %s, the trial for %s ends in %d day(s), on %s.", firstName(name), org, daysLeft, ends.Format("2 January 2006")),
		[]string{
			"Choose a plan to keep your properties, team, and work history exactly as they are. Nothing is deleted when you upgrade.",
			"Prefer to talk it through first? Book a demo and we will walk you through the options.",
		},
		"Choose a plan", appURL+"/settings/plan",
		"")
	return mailer.Message{Subject: "Your BuildingVision trial ends soon", Text: text, HTML: htm}
}

func trialExpiredEmail(name, org, appURL string) mailer.Message {
	text, htm := layout(
		"Your trial has expired",
		fmt.Sprintf("Hi %s, the free trial for %s has ended.", firstName(name), org),
		[]string{
			"Your data is kept safe. Choose a plan to pick up right where your team left off.",
			"If BuildingVision was not the right fit, we would love to hear why. Just reply to this email.",
		},
		"Choose a plan", appURL+"/settings/plan",
		"")
	return mailer.Message{Subject: "Your BuildingVision trial has expired", Text: text, HTML: htm}
}

func planChosenEmail(name, org, plan string) mailer.Message {
	text, htm := layout(
		"Thanks for choosing BuildingVision",
		fmt.Sprintf("Hi %s, %s is now on the %s plan.", firstName(name), org, strings.Title(plan)),
		[]string{
			"Our team will reach out shortly to confirm billing details and help with anything you need. Your workspace stays fully available in the meantime.",
		},
		"", "",
		"")
	return mailer.Message{Subject: "Your BuildingVision plan is confirmed", Text: text, HTML: htm}
}

func demoRequestEmail(req DemoRequestInput) mailer.Message {
	lines := []string{
		"Name: " + req.FullName,
		"Email: " + req.Email,
	}
	if req.Company != nil && *req.Company != "" {
		lines = append(lines, "Company: "+*req.Company)
	}
	if req.Phone != nil && *req.Phone != "" {
		lines = append(lines, "Phone: "+*req.Phone)
	}
	if req.PropertyProfile != nil && *req.PropertyProfile != "" {
		lines = append(lines, "Property profile: "+*req.PropertyProfile)
	}
	if req.PropertyCount != nil {
		lines = append(lines, fmt.Sprintf("Number of properties: %d", *req.PropertyCount))
	}
	if req.Message != nil && *req.Message != "" {
		lines = append(lines, "Message: "+*req.Message)
	}
	if req.SourcePage != nil && *req.SourcePage != "" {
		lines = append(lines, "Source page: "+*req.SourcePage)
	}
	text, htm := layout("New demo request", "A visitor asked for a BuildingVision demo.", lines, "", "", "")
	return mailer.Message{Subject: "Demo request: " + req.FullName, Text: text, HTML: htm}
}

func demoConfirmationEmail(name string) mailer.Message {
	text, htm := layout(
		"We received your demo request",
		fmt.Sprintf("Hi %s, thanks for your interest in BuildingVision.", firstName(name)),
		[]string{
			"Someone from our team will reach out within one business day to find a time that works for you.",
			"In the meantime, feel free to start a free trial and explore the product at your own pace.",
		},
		"", "",
		"")
	return mailer.Message{Subject: "Your BuildingVision demo request", Text: text, HTML: htm}
}
