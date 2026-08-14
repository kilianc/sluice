-- The database this tutorial filters. Nothing here knows about Sluice: it is an
-- ordinary issue tracker, with the joins, enums, JSONB and computed values a
-- real schema has.

CREATE TABLE team (
  id   serial PRIMARY KEY,
  name text   NOT NULL UNIQUE
);

CREATE TABLE app_user (
  id           serial PRIMARY KEY,
  email        text   NOT NULL UNIQUE,
  display_name text   NOT NULL,
  team_id      int    REFERENCES team (id)
);

CREATE TYPE ticket_status AS ENUM ('open', 'in_progress', 'blocked', 'closed');

CREATE TABLE ticket (
  id          uuid          PRIMARY KEY DEFAULT gen_random_uuid(),
  title       text          NOT NULL,
  status      ticket_status NOT NULL,
  priority    int           NOT NULL CHECK (priority BETWEEN 1 AND 5),
  assignee_id int           REFERENCES app_user (id),
  team_id     int           REFERENCES team (id),
  created_at  timestamptz   NOT NULL DEFAULT now(),
  due_at      timestamptz,
  meta        jsonb         NOT NULL DEFAULT '{}'
);

CREATE TABLE comment (
  id         serial      PRIMARY KEY,
  ticket_id  uuid        NOT NULL REFERENCES ticket (id) ON DELETE CASCADE,
  author_id  int         NOT NULL REFERENCES app_user (id),
  body       text        NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO team (id, name) VALUES (1, 'platform'), (2, 'billing'), (3, 'growth');

INSERT INTO app_user (id, email, display_name, team_id) VALUES
  (1, 'ada@example.com',   'Ada Lovelace',   1),
  (2, 'grace@example.com', 'Grace Hopper',   1),
  (3, 'kim@example.com',   'Kim Nakamura',   2),
  (4, 'omar@example.com',  'Omar Haddad',    3);

INSERT INTO ticket (id, title, status, priority, assignee_id, team_id, created_at, due_at, meta) VALUES
  ('11111111-1111-4111-8111-111111111101', 'Checkout times out under load', 'open',        1, 3, 2, now() - interval '2 days',  now() - interval '1 day',  '{"source":"pagerduty"}'),
  ('11111111-1111-4111-8111-111111111102', 'Invoice PDF missing tax line',   'in_progress', 2, 3, 2, now() - interval '9 days',  now() + interval '3 days', '{"source":"support"}'),
  ('11111111-1111-4111-8111-111111111103', 'Rotate signing keys',            'open',        1, 1, 1, now() - interval '40 days', now() - interval '9 days', '{"source":"security"}'),
  ('11111111-1111-4111-8111-111111111104', 'Flaky deploy on cold cache',     'blocked',     3, 2, 1, now() - interval '15 days', NULL,                      '{"source":"ci"}'),
  ('11111111-1111-4111-8111-111111111105', 'Onboarding email bounces',       'open',        4, 4, 3, now() - interval '3 hours', now() + interval '7 days', '{"source":"support"}'),
  ('11111111-1111-4111-8111-111111111106', 'Upgrade Postgres to 16',         'closed',      3, 1, 1, now() - interval '120 days', now() - interval '90 days','{"source":"planning"}'),
  ('11111111-1111-4111-8111-111111111107', 'Rate limit the search endpoint', 'open',        2, 2, 1, now() - interval '6 days',  now() + interval '1 day',  '{"source":"pagerduty"}'),
  ('11111111-1111-4111-8111-111111111108', 'Refund webhook retries forever', 'in_progress', 1, 3, 2, now() - interval '1 day',   now() - interval '2 hours','{"source":"pagerduty"}'),
  ('11111111-1111-4111-8111-111111111109', 'Docs: filter syntax page',       'open',        5, 4, 3, now() - interval '70 days', NULL,                      '{"source":"internal"}'),
  ('11111111-1111-4111-8111-111111111110', 'Trial expiry banner is wrong',   'closed',      4, 4, 3, now() - interval '25 days', now() - interval '20 days','{"source":"support"}');

INSERT INTO comment (ticket_id, author_id, body) VALUES
  ('11111111-1111-4111-8111-111111111101', 1, 'Reproduced at 2k rps.'),
  ('11111111-1111-4111-8111-111111111101', 3, 'Pool exhaustion, not the query.'),
  ('11111111-1111-4111-8111-111111111101', 2, 'Raising the pool size for now.'),
  ('11111111-1111-4111-8111-111111111102', 3, 'Only affects EU invoices.'),
  ('11111111-1111-4111-8111-111111111104', 2, 'Waiting on the cache team.'),
  ('11111111-1111-4111-8111-111111111104', 1, 'Still blocked.'),
  ('11111111-1111-4111-8111-111111111107', 2, 'Token bucket per key?'),
  ('11111111-1111-4111-8111-111111111108', 3, 'Idempotency key is missing.'),
  ('11111111-1111-4111-8111-111111111108', 1, 'Patch is up.'),
  ('11111111-1111-4111-8111-111111111108', 4, 'Verified in staging.');
