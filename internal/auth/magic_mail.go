package auth

import (
	"fmt"
	"html"
)

// magicMailText renders the plain-text sign-in email carrying both the
// grouped 8-digit code and the one-click magic link.
func magicMailText(link, code string, minutes int) string {
	return fmt.Sprintf(
		"Your swegai sign-in code:\n\n    %s\n\nOr open this link:\n\n%s\n\nThe code and link each work once and expire in %d minutes.\nIf you did not request this, ignore this email.",
		code, link, minutes,
	)
}

// magicMailHTML renders the transactional sign-in email. All styles are
// inline for email-client compatibility; the link is HTML-escaped at every
// interpolation point.
func magicMailHTML(link, code string, minutes int) string {
	escLink := html.EscapeString(link)
	escCode := html.EscapeString(code)
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Your swegai sign-in code</title>
</head>
<body style="margin:0;padding:0;background:#f4f5f7;font-family:Arial,Helvetica,sans-serif;">
<div style="display:none;max-height:0;overflow:hidden;">Your swegai sign-in code is %s. It expires in %d minutes.</div>
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="padding:24px;">
<tr><td align="center">
<table role="presentation" width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%%;background:#ffffff;border-radius:8px;padding:32px;">
<tr><td>
<div style="font-size:18px;font-weight:bold;color:#111827;">swegai</div>
<h1 style="font-size:22px;color:#111827;margin:16px 0;">Sign in to swegai</h1>
<p style="font-size:16px;color:#374151;line-height:1.5;margin:0 0 24px;">Use this code to sign in to your swegai account. It expires in %d minutes.</p>
<div style="text-align:center;background:#f4f5f7;border-radius:6px;padding:14px 24px;font-family:'Courier New',Courier,monospace;font-size:32px;font-weight:bold;letter-spacing:4px;color:#111827;">%s</div>
<p style="text-align:center;font-size:12px;color:#6b7280;margin:24px 0;">or</p>
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0"><tr><td align="center">
<a href="%s" style="display:inline-block;background:#0a2540;color:#ffffff;padding:12px 28px;border-radius:6px;text-decoration:none;font-weight:bold;font-size:16px;">Sign in to swegai</a>
</td></tr></table>
<p style="font-size:13px;color:#6b7280;line-height:1.5;margin:24px 0 0;">If the button does not work, paste this link into your browser:<br><a href="%s" style="color:#6b7280;word-break:break-all;">%s</a></p>
<p style="font-size:12px;color:#888888;line-height:1.5;margin:24px 0 0;">The code and link each work once. If you did not request this, you can ignore this email.</p>
</td></tr>
</table>
<p style="text-align:center;font-size:11px;color:#999999;margin:16px 0 0;">Sent by swegai</p>
</td></tr>
</table>
</body>
</html>`, escCode, minutes, minutes, escCode, escLink, escLink, escLink)
}

// displayMagicCode groups the 8-digit code for readability: "1234 5678".
func displayMagicCode(digits string) string {
	if len(digits) != magicCodeDigits {
		return digits
	}
	return digits[:4] + " " + digits[4:]
}
