# Holy Hymns privacy information

This text documents the implemented data flows. Before publishing, the operator must supply a real support/privacy contact and host a publicly accessible privacy policy with its legal identity, actual server region and configured retention periods.

- Anyone can read published lyrics without an account. Preferences and guest bookmarks are stored on the device.
- An optional account stores name, email, provider subject identifiers, password hash (email accounts), account status, active sessions and synchronized bookmark IDs on the operator's server.
- iOS/Android store session tokens in platform secure storage. The optional browser preview uses tab-session storage; signing out removes the token.
- Choosing Google or Apple sign-in sends credentials to that provider and a signed identity token plus a one-use nonce to the Holy Hymns API. Apple also returns an authorization code so the server can revoke the grant on account deletion.
- Verification, reset and invitation emails use the operator's SMTP provider. The app includes email token-entry/deep-link workflows.
- Analytics are opt-in and initially disabled. Events include song/category IDs or search-result counts; raw queries, emails and advertising identifiers are not included. Staff events are excluded.
- The app has no advertisements, cross-app tracking, sale of personal information or remote font service. It opens external song/karaoke links only when the user chooses them; those websites have their own privacy policies.
- Account deletion is available in Account. The backend removes account records and bookmarks, revokes active sessions and Apple grants as applicable. Encrypted backups and minimized security/audit records expire according to configured retention; the launch privacy policy must state those periods.
- Administrative actions are logged for security. Backend connection logs may contain network metadata. Access is restricted to the operator.

To complete App Store privacy and Play Data Safety forms, use the deployed behavior and SDK versions, including optional account information, user IDs, opt-in product interactions and network/security logs. Do not claim that no data is collected. Verify all third-party disclosures against the production configuration before submission.
