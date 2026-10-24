-- 0001_init : schéma initial (utilisateurs, équipements, tickets, historique, commentaires)

CREATE TABLE users (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         TEXT        NOT NULL,
    full_name     TEXT        NOT NULL,
    role          TEXT        NOT NULL CHECK (role IN ('employee', 'technician', 'admin')),
    password_hash TEXT        NOT NULL,
    active        BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

CREATE TABLE equipment (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    asset_tag           TEXT        NOT NULL UNIQUE,
    name                TEXT        NOT NULL,
    type                TEXT        NOT NULL CHECK (type IN ('computer', 'network', 'printer', 'ticket_machine', 'other')),
    location            TEXT        NOT NULL DEFAULT '',
    responsible_id      BIGINT      REFERENCES users (id) ON DELETE SET NULL,
    status              TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'in_repair', 'retired')),
    last_maintenance_at TIMESTAMPTZ,
    next_maintenance_at TIMESTAMPTZ,
    notes               TEXT        NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX equipment_responsible_idx ON equipment (responsible_id);
CREATE INDEX equipment_status_idx ON equipment (status);

CREATE TABLE tickets (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title        TEXT        NOT NULL,
    description  TEXT        NOT NULL,
    category     TEXT        NOT NULL CHECK (category IN ('computer', 'network', 'printer', 'ticketing_counter', 'other')),
    priority     TEXT        NOT NULL DEFAULT 'medium' CHECK (priority IN ('low', 'medium', 'high', 'critical')),
    status       TEXT        NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'in_progress', 'done')),
    requester_id BIGINT      NOT NULL REFERENCES users (id),
    assignee_id  BIGINT      REFERENCES users (id) ON DELETE SET NULL,
    equipment_id BIGINT      REFERENCES equipment (id) ON DELETE SET NULL,
    ai_category  TEXT        CHECK (ai_category IN ('computer', 'network', 'printer', 'ticketing_counter', 'other')),
    ai_priority  TEXT        CHECK (ai_priority IN ('low', 'medium', 'high', 'critical')),
    ai_summary   TEXT,
    ai_source    TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at   TIMESTAMPTZ,
    resolved_at  TIMESTAMPTZ,
    -- resolved_at est renseigné si et seulement si le ticket est terminé
    CONSTRAINT tickets_resolved_consistent CHECK ((status = 'done') = (resolved_at IS NOT NULL))
);
CREATE INDEX tickets_status_idx ON tickets (status);
CREATE INDEX tickets_category_idx ON tickets (category);
CREATE INDEX tickets_requester_idx ON tickets (requester_id);
CREATE INDEX tickets_assignee_idx ON tickets (assignee_id);
CREATE INDEX tickets_equipment_idx ON tickets (equipment_id);
CREATE INDEX tickets_created_idx ON tickets (created_at DESC);

CREATE TABLE ticket_status_history (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id   BIGINT      NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    from_status TEXT        CHECK (from_status IN ('new', 'in_progress', 'done')),
    to_status   TEXT        NOT NULL CHECK (to_status IN ('new', 'in_progress', 'done')),
    changed_by  BIGINT      NOT NULL REFERENCES users (id),
    changed_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ticket_status_history_ticket_idx ON ticket_status_history (ticket_id, changed_at);

CREATE TABLE comments (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id  BIGINT      NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    author_id  BIGINT      NOT NULL REFERENCES users (id),
    body       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX comments_ticket_idx ON comments (ticket_id, created_at);
