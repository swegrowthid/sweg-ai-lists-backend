package auth

import (
	"fmt"
	"html"
)

// magicMailText renders the plain-text sign-in email. It repeats every fact
// that also appears in the HTML part so text-only clients lose nothing: the
// grouped code, the full link, the expiry, and the "ignore this" line.
func magicMailText(link, code string, minutes int) string {
	return fmt.Sprintf(
		"Sign in to swegai\n\n"+
			"Enter this code in the app:\n\n"+
			"%s\n\n"+
			"Or open this link:\n\n"+
			"%s\n\n"+
			"The code and the link each work once and expire in %d minutes.\n"+
			"If you did not ask to sign in, ignore this email. Nobody can sign in\n"+
			"without the code or the link.\n\n"+
			"-\n"+
			"swegai\n"+
			"This is an automated message. Replies are not read.",
		code, link, minutes,
	)
}

// magicMailHTML renders the sign-in email as one single-column card. The
// markup stays plain on purpose: no external images, no tracking pixel, no
// scripts, and no marketing blocks. Filters score those as bulk mail. Every
// style is inline for email-client compatibility, and every interpolation is
// HTML-escaped.
func magicMailHTML(link, code string, minutes int) string {
	escLink := html.EscapeString(link)
	escCode := html.EscapeString(code)
	const body = "margin:0;padding:0;background:#f4f5f7;"
	const cell = "font-family:Arial,Helvetica,sans-serif;"
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light">
<title>Sign in to swegai</title>
</head>
<body style="%s">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;">Your swegai sign-in code expires in %d minutes.</div>
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="background:#f4f5f7;">
<tr><td align="center" style="padding:32px 16px;">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="max-width:520px;background:#ffffff;border:1px solid #e5e7eb;border-radius:8px;">
<tr><td style="%spadding:32px;">
<p style="margin:0 0 8px;font-size:15px;font-weight:bold;color:#111827;">swegai</p>
<h1 style="margin:0 0 12px;font-size:20px;line-height:1.3;color:#111827;">Sign in to swegai</h1>
<p style="margin:0 0 24px;font-size:15px;line-height:1.5;color:#374151;">Enter this code in the sign-in screen. It expires in %d minutes.</p>
<p style="margin:0 0 24px;text-align:center;font-family:Consolas,'Courier New',monospace;font-size:30px;line-height:1.2;font-weight:bold;letter-spacing:6px;color:#111827;">%s</p>
<p style="margin:0 0 24px;font-size:15px;line-height:1.5;color:#374151;">Or use this link to sign in:</p>
<p style="margin:0 0 24px;"><a href="%s" style="color:#0a58ca;font-size:15px;word-break:break-all;">%s</a></p>
<p style="margin:0 0 8px;font-size:13px;line-height:1.5;color:#4b5563;">The code and the link each work once. A new request cancels both.</p>
<p style="margin:0;font-size:13px;line-height:1.5;color:#4b5563;">If you did not ask to sign in, ignore this email. Nobody can sign in without the code or the link.</p>
</td></tr>
<tr><td style="%spadding:0 32px 28px;">
<hr style="border:0;border-top:1px solid #e5e7eb;margin:0 0 16px;">
<p style="margin:0;font-size:12px;line-height:1.5;color:#6b7280;">swegai sent this automated message about a sign-in request for your account. Replies are not read.</p>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`, body, minutes, cell, minutes, escCode, escLink, escLink, cell)
}

// displayMagicCode groups the 8-digit code for readability: "1234 5678".
func displayMagicCode(digits string) string {
	if len(digits) != magicCodeDigits {
		return digits
	}
	return digits[:4] + " " + digits[4:]
}

// MagicMailPreview renders the exact sign-in email that Request sends, so
// tooling and examples can inspect or deliver the real body without a store.
func MagicMailPreview(link, code string, minutes int) (subject, text, html string) {
	return magicMailSubject, magicMailText(link, code, minutes), magicMailHTML(link, code, minutes)
}
