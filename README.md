# SETRAM Helpdesk

A web-based IT helpdesk API built with Go to manage IT support tickets, users, and equipment records.

> **Project status:** Independent portfolio project. This is not an official SETRAM product.

## Overview

SETRAM Helpdesk is designed to centralize IT support workflows. It provides a REST API for authentication, user management, and ticket handling, with additional endpoints for equipment management, service statistics, and optional AI-assisted incident classification.

The project uses Go for the backend, PostgreSQL for persistent storage, and Docker Compose to run the application and database.

## Features

### Authentication and Users
- User login and current-user endpoints.
- JWT-based authentication.
- User management endpoints.
- Role-based access controls where configured.

### Ticket Management
- Create and retrieve support tickets.
- Update ticket information.
- Change ticket status.
- Add comments to tickets.
- Request AI-assisted ticket classification.

### Additional Capabilities
- Equipment management endpoints.
- Service statistics endpoint.
- Optional AI provider configuration with local classification fallback.

The exact request formats, validation rules, and access requirements are documented in the API specification and implemented in the application handlers.

## Technology Stack

- **Backend:** Go 1.24
- **Database:** PostgreSQL 16
- **Database Driver:** pgx v5
- **API:** REST
- **API Documentation:** OpenAPI 3.0.3 and Swagger UI
- **Authentication:** JWT-based authentication
- **Frontend:** HTML, CSS, and JavaScript
- **Containerization:** Docker and Docker Compose
- **Testing:** Go testing, race detection, and PostgreSQL integration tests
- **Static Analysis:** `go vet`
- **Continuous Integration:** GitHub Actions

## Architecture

```text
.
├── cmd/
│   └── server/          # Application entry point
├── internal/
│   ├── ai/              # AI-assisted classification
│   ├── auth/            # Authentication and password hashing
│   ├── config/          # Application configuration
│   ├── domain/          # Domain models and business rules
│   ├── httpapi/         # HTTP handlers and REST API
│   ├── seed/            # Development/demo data
│   ├── service/         # Application services
│   └── store/
│       ├── memory/      # In-memory storage
│       └── postgres/    # PostgreSQL storage
├── migrations/          # Database migrations
├── web/
│   └── static/          # Web assets and Swagger UI files
├── docs/
│   └── openapi.yaml     # OpenAPI specification
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── go.mod
```

## API Documentation

The project includes an OpenAPI specification and an interactive Swagger UI.

When the application is running locally on port `8080`, open:

- **Swagger UI:** http://localhost:8080/swagger.html
- **OpenAPI specification:** http://localhost:8080/openapi.yaml

Swagger UI allows developers to:
- Explore available API endpoints.
- Inspect request parameters and response schemas.
- Review example responses.
- Try API requests against the running application.

If you configure a different host port through `APP_PORT`, replace `8080` in these addresses with your configured port.

## API Overview

The API includes endpoints for health checks, authentication, users, tickets, equipment, statistics, and AI-assisted classification.

| Method | Endpoint | Purpose |
|---|---|---|
| `GET` | `/healthz` | Application health check |
| `POST` | `/api/auth/login` | Authenticate a user |
| `GET` | `/api/me` | Retrieve the current user's information |
| `GET`, `POST` | `/api/users` | List and create users |
| `GET`, `POST` | `/api/tickets` | List and create tickets |
| `GET`, `PATCH` | `/api/tickets/{id}` | Retrieve or update a ticket |
| `POST` | `/api/tickets/{id}/status` | Change ticket status |
| `POST` | `/api/tickets/{id}/comments` | Add a ticket comment |
| `POST` | `/api/tickets/classify` | Request ticket classification |
| `GET`, `POST` | `/api/equipment` | List and create equipment records |
| `GET`, `PUT` | `/api/equipment/{id}` | Retrieve or update equipment |
| `GET` | `/api/stats` | Retrieve service statistics |

Some endpoints may require authentication or specific roles. Consult the OpenAPI specification and application handlers for the exact requirements.

## Prerequisites

To run the application with Docker Compose, install:

- Docker
- Docker Compose

For local Go development, install Go 1.24 or a compatible version.

## Getting Started

### 1. Clone the repository

```bash
git clone https://github.com/meriemotemani12/setram-helpdesk.git
cd setram-helpdesk
```

### 2. Configure the environment

Create a local environment file:

```bash
cp .env.example .env
```

Edit `.env` and configure the required values, including:

- `POSTGRES_PASSWORD`
- `JWT_SECRET`
- `ADMIN_EMAIL`
- `ADMIN_PASSWORD`

The JWT signing secret must satisfy the application's configured minimum length. Use strong, unique secrets and never commit `.env` to version control.

Optional AI provider settings include `ANTHROPIC_API_KEY`, `GROQ_API_KEY`, and `GROQ_MODEL`.

### 3. Start the application

```bash
docker compose up --build -d
```

Check the services:

```bash
docker compose ps
```

View application logs:

```bash
docker compose logs -f app
```

By default, the application is exposed on port `8080`. If `APP_PORT` is configured to another value in `.env`, use that port instead.

Open the application:

```text
http://localhost:8080
```

API documentation:

```text
http://localhost:8080/swagger.html
```

Use the administrator credentials configured in your local environment to sign in.

### 4. Stop the application

```bash
docker compose down
```

The named PostgreSQL volume is retained when using `docker compose down`. Removing the volume will delete the persisted database data, so only do that when you intentionally want to reset it.

## Development

The repository includes a development target in the Makefile:

```bash
make run-dev
```

The development configuration uses in-memory storage, so data may be lost when the process stops.

Review the Makefile and `.env.example` before running development or integration commands.

## Testing

Run the Go test suite:

```bash
go test ./...
```

Run static analysis:

```bash
go vet ./...
```

The GitHub Actions workflow also defines race-enabled tests and PostgreSQL integration tests.

To run PostgreSQL integration tests locally, configure the test database according to the requirements in the repository's CI workflow.

## Continuous Integration

GitHub Actions is configured to run on pushes and pull requests.

The CI workflow includes:

- Go 1.24 setup.
- Go module dependency maintenance.
- Static analysis with `go vet`.
- Tests with the Go race detector.
- PostgreSQL integration tests using PostgreSQL 16.

These checks help identify potential issues before changes are merged. Their current status can be verified in the repository's GitHub Actions tab.

## Security Considerations

- Keep `.env` and all secrets out of version control.
- Use strong database credentials and a secure JWT signing secret.
- Avoid putting passwords, personal information, or confidential organizational data in tickets.
- Review privacy requirements before sending ticket content to an external AI provider.
- For deployment beyond local development, configure HTTPS and review the application's production security settings.

## Roadmap

Potential future improvements include:

- Notifications for critical incidents.
- File attachments for incident reports.
- Preventive maintenance scheduling.
- Duplicate-ticket detection.
- CSV/PDF report exports.
- Additional operational metrics and dashboards.

These items are proposals, not claims about completed functionality.

## Disclaimer

This is an independent portfolio project and is not an official SETRAM application. It is not affiliated with or endorsed by SETRAM.

## License

No license has been specified yet. Unless a license is added to the repository, reuse and redistribution are not automatically permitted.