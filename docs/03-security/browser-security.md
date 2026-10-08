# Browser Security

**Status:** Draft

Sessions use secure, HttpOnly, SameSite cookies; rotation; idle and absolute timeouts; device/session visibility; and revocation. State-changing cookie-authenticated requests receive CSRF protection.

The application uses a restrictive Content Security Policy, output encoding, safe HTML sanitization, clickjacking protection, trusted origins, safe redirects, upload controls, dependency review, and no secrets in browser storage. Sensitive operations may require recent authentication.
