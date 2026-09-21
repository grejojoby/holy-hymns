# Mobile release and App Review

This application is a lyric reader with optional accounts and a server-authorized editorial workspace. Management appears after an administrator signs into the normal account screen; it is not a secret bypass or a feature concealed from App Review.

Before submission:

- Set the final iOS bundle identifier, Android package identifier, app-store metadata and production API URL. Build locally with Xcode/Android Studio; paid hosted builds are not required.
- Test browsing, Malayalam rendering, Manglish switching, large accessibility text, dark mode, favorites and live updates on a real iPhone and an Android device/emulator.
- Enable Google and Sign in with Apple only after their configured production audiences, native identifiers and signing settings have been tested. Test invalid tokens, account linking, password reset and session revocation.
- Supply a working reviewer reader account and a separate restricted admin review account in App Review notes. Explain the account screen and exact steps to reach Manage Holy Hymns. Avoid providing the protected owner or VM credentials.
- Demonstrate draft, preview, publish and unpublish behavior using review-only sample content. Explain that account roles are enforced on the backend and that public readers cannot access drafts.
- Verify account deletion is available in the app and removes identity/personal data according to the implemented retention policy. Provide the account-deletion web instructions/URL required by the target store, and test the production process.
- Publish a privacy policy and support/contact page, and enter their real URLs in store metadata. Describe account data, optional aggregate analytics, external media links, email delivery and user rights accurately. Complete Apple privacy labels and Google Data Safety from the final implementation.
- Confirm permission to distribute each published lyric and preserve available credits. Blogger ownership identifies the authorized import source but does not automatically establish rights to every third-party composition.
- Verify the production domain, SMTP sender, restore drill, failure monitoring and VM load test. Confirm no development password, test provider audience or local URL remains in release builds.

Store memberships and domain costs are independent of the application's free/open-source backend stack. App Review remains an external approval process; source code and a successful simulator build do not guarantee acceptance.

Consult the current [Apple App Review Guidelines](https://developer.apple.com/app-store/review/guidelines/) and [Google Play user-data policy](https://support.google.com/googleplay/android-developer/answer/10144311) immediately before submission because store requirements can change.
