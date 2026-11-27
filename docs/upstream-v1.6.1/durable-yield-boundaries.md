# Durable discards a continuation when reset or follow-up wins

The unchanged `harness-inbox.test.ts` cases at lines 522–544 and 702–718 passed on the pinned Node fixture. The test, chat and harness support and Vitest config matched Git. The other 35 tests in the file were unselected. Five focused Go groups passed on the unchanged native checkpoint `5c86bc76`.

The public SQLite tests hold the first answer until the competing submission is queued. Both original inputs settle against the first answer, and neither transcript contains the hook's `more` continuation. Reset writes its own marker and makes one provider request. Follow-up runs its own second request without `more` and settles against the second answer. These checks map the visible outcomes, not the upstream tagged TypeScript hook union.

The native lane passed six groups twice with shuffle seed 42, vet and Staticcheck. Bounded scratch controls ignoring either reset or follow-up failed on the exact extra continuation entry and closed their owned streams. No production scheduler, storage, or ownership rules changed.
