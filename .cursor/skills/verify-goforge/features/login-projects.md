# Log in and create a project

A registered user signs in with their email and password, opens the projects list, and creates a project that then appears in that list.

## Sub-features

- `login` accepts the email and password and opens the dashboard.
- `projects-list` shows the `Projects` page.
- `project-create` saves a project from the new project form and redirects to its page.
- `project-listed` shows the new project name on the `Projects` page.
- `project-delete-cleanup` removes the fixture through the UI after all dependent drives finish.

## How to get to it (user POV)

- Open `http://127.0.0.1:8080/login`. The heading is `Sign in to GoForge`.
- Submit email and password. The browser lands on `/dashboard`.
- In the sidebar, choose `Projects`, then `New Project`. Submit `Create Project`.

## Driving it with verify.sh

Preconditions:

- `drive-register` passed on this volume. Its password is in `/tmp/goforge-verify/state/register.password`.

- **Log in.** Run `.cursor/skills/verify-goforge/scripts/verify.sh drive-login`. It loads `/login` for a CSRF cookie in a fresh jar and posts `email` (the lowercase `GOFORGE_ADMIN_EMAIL`) and `password`. The POST returns `303` to `/dashboard`, and `GET /dashboard` returns `200`.
- **List.** `GET /projects` returns `200` and the body contains `View and manage your application projects.`
- **Create.** `POST /projects` with `name=verify-smoke`, `repository_url=https://github.com/raythurman2386/goforge.git`, `default_branch=main`, `dockerfile_path=Dockerfile`, `build_context=.` returns `303` and `Location` is `/projects/{id}`. `GET` that path returns `200`.
- **Second view.** `GET /projects` contains `verify-smoke`, and `select count(*) from projects where name = 'verify-smoke'` prints `1`.
- **Proof.** `/tmp/goforge-verify/proof/login-verdict.txt`.
- **Cleanup.** After `drive-mcp-http` and any other drive that uses `verify-smoke`, run `verify.sh drive-login-cleanup`. It deletes the project through `DELETE /projects/{id}` with `confirm_name=verify-smoke`, then proves the database row is gone. Its verdict is `/tmp/goforge-verify/proof/login-cleanup-verdict.txt`.

## Gotchas

- A wrong password returns `401` and re-renders the login page with `Invalid email or password`. It is not a redirect.
- Creating a project does not clone or build. Deploy is a separate journey.
- The project form needs a CSRF token in `csrf_token`. Read it again from the jar after login.
- Do not run the cleanup before other drives that read the `verify-smoke` project or its production environment.
