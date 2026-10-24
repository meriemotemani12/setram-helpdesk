# SETRAM Helpdesk

A web-based IT helpdesk and equipment management system built with Go. It helps organizations manage IT support tickets, track equipment maintenance, monitor service performance, and classify reported issues.

> **Project status:** Portfolio project — not an official SETRAM product.

## Overview

SETRAM Helpdesk provides a centralized workflow for handling IT incidents and maintaining an equipment inventory. It includes role-based access, ticket status tracking, service analytics, and optional AI-assisted incident classification.

The interface supports **French and Arabic**, including right-to-left layout for Arabic.

## Features

### Ticket Management
- Create and track IT support tickets.
- Assign tickets to technicians and update their status.
- Add comments and maintain ticket status history.
- Apply role-based access rules to ticket operations.

### Equipment Inventory
- Maintain equipment records and their locations.
- Track responsible personnel and maintenance dates.
- Identify overdue maintenance.

### Analytics
- Review frequently reported incident categories.
- Monitor average resolution and response times.
- Identify equipment with recurring incidents.

### AI-Assisted Classification
- Suggest incident categories and priorities from ticket descriptions.
- Use an optional external AI provider when configured.
- Fall back to local classification rules when an external AI key is not configured.
- Keep the final classification decision with the user.

### Security
- JWT-based authentication.
- Password hashing.
- Role-based authorization.
- Login attempt rate limiting.
- HTTP security headers and a restrictive Content Security Policy.

## Technology Stack

- **Backend:** Go
- **Database:** PostgreSQL
- **API:** REST
- **Authentication:** JSON Web Tokens (JWT)
- **Frontend:** HTML, CSS, JavaScript
- **Deployment:** Docker and Docker Compose
- **Testing:** Go testing framework
- **CI:** GitHub Actions

## Architecture

```text
.
├── cmd/
│   └── server/       # Application entry point
├── internal/
│   ├── ai/           # Incident classification
│   ├── auth/         # Authentication and password hashing
│   ├── config/       # Environment-based configuration
│   ├── domain/       # Domain models and business rules
│   ├── httpapi/      # HTTP handlers and REST API
│   ├── seed/         # Development/demo data
│   ├── service/      # Application services
│   └── store/
│       ├── memory/   # In-memory storage
│       └── postgres/ # PostgreSQL storage
├── migrations/       # Database migrations
├── web/
│   └── static/       # Frontend assets
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── go.mod
```

## Prerequisites

To run the containerized application, install:

- Docker
- Docker Compose

For local Go development, install a compatible Go version as specified in `go.mod`.

## Getting Started

### 1. Clone the repository

```bash
git clone https://github.com/meriemotemani12/setram-helpdesk.git
cd setram-helpdesk
```

### 2. Configure environment variables

Create your local environment file:

```bash
cp .env.example .env
```

Edit `.env` and replace the example values with your own configuration. Set a strong database password, a secure JWT signing secret, and administrator credentials.

Never commit `.env` or share its contents.

### 3. Start the application

```bash
docker compose up --build -d
```

Check the service status:

```bash
docker compose ps
```

View application logs:

```bash
docker compose logs -f app
```

Open the application using the host port configured by `APP_PORT` in `.env`. For example, if `APP_PORT=8081`, visit:

```text
http://localhost:8081
```

Sign in using the administrator credentials configured in your local environment file.

To stop the services:

```bash
docker compose down
```

The database volume is retained when using `docker compose down`. To remove persistent database data as well, Docker provides a separate volume-removal option; use it only when you intentionally want to delete that data.

## Development

Run the development target defined in the Makefile:

```bash
make run-dev
```

The development configuration uses in-memory storage, so data may be lost when the process stops. Use only the demo credentials and configuration intended for local development.

Inspect the available Make targets in the `Makefile` before running other development or integration commands.

## Testing

Run the Go test suite:

```bash
go test ./...
```

If dependencies need to be downloaded:

```bash
go mod download
```

The repository also includes a GitHub Actions workflow for continuous integration.

## API Overview

The REST API includes endpoints for authentication, users, tickets, equipment, analytics, and health checks.

| Method | Endpoint | Purpose |
|---|---|---|
| `POST` | `/api/auth/login` | Authenticate a user |
| `GET` | `/api/me` | Retrieve the current user's information |
| `GET`, `POST` | `/api/users` | List and create users |
| `GET`, `POST` | `/api/tickets` | List and create tickets |
| `GET`, `PATCH` | `/api/tickets/{id}` | Retrieve or update a ticket |
| `POST` | `/api/tickets/classify` | Request incident classification |
| `POST` | `/api/tickets/{id}/status` | Change ticket status |
| `POST` | `/api/tickets/{id}/comments` | Add a ticket comment |
| `GET`, `POST` | `/api/equipment` | List and create equipment records |
| `GET`, `PUT` | `/api/equipment/{id}` | Retrieve or update an equipment record |
| `GET` | `/api/stats` | Retrieve service statistics |
| `GET` | `/healthz` | Check application health |

Endpoints may require authentication and specific roles. Refer to the application handlers for the exact request formats, validation rules, and authorization requirements.

## Environment Configuration

The `.env.example` file documents the supported configuration variables, including:

- PostgreSQL credentials
- JWT signing secret
- Initial administrator account
- Application host port
- Optional AI provider API key and model
- Optional development/demo data settings

Use placeholder values in documentation and examples. Never publish real credentials, API keys, tokens, or production configuration.

## Security Considerations

- Use strong, unique credentials and a randomly generated JWT signing secret.
- Keep `.env` out of version control.
- Do not include passwords, personal information, or confidential company data in ticket descriptions.
- When an external AI provider is enabled, ticket information sent to that provider should be reviewed for privacy and data-handling requirements.
- For deployment beyond local development, configure HTTPS through an appropriately secured reverse proxy and review the application's production security settings.

## Roadmap

Potential future enhancements include:

- Notifications for critical incidents.
- File attachments for incident reports.
- Automated preventive maintenance scheduling.
- Duplicate-ticket detection.
- CSV/PDF report exports.
- Metrics and dashboards for operational monitoring.

These are potential improvements and are not necessarily implemented in the current version.

## Disclaimer

This is an independent portfolio project and is not an official SETRAM application. The project name and visual identity are used for demonstration purposes only. It is not affiliated with or endorsed by SETRAM.

## License

No license has been specified yet. Unless a license is added to the repository, reuse and redistribution are not automatically permitted.
