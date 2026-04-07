-- Idempotent seed data for HarborWorks. Safe to run on every startup.
-- Bookings are not seeded here because they require a real user_id; the
-- application creates a default admin user post-seed (see cmd/server/main.go).

INSERT INTO resources (id, name, description, capacity)
VALUES
    ('aaaa1111-0000-0000-0000-000000000001', 'Slip A1',          'North dock, 30ft slip',          1),
    ('aaaa1111-0000-0000-0000-000000000002', 'Slip A2',          'North dock, 40ft slip',          1),
    ('aaaa1111-0000-0000-0000-000000000003', 'Mooring M7',       'Outer harbor mooring ball',      1),
    ('bbbb2222-0000-0000-0000-000000000001', 'Lighthouse Room',  'Conference room, seats 12',     12),
    ('bbbb2222-0000-0000-0000-000000000002', 'Sunset Deck',      'Open-air deck for events',      40)
ON CONFLICT (id) DO NOTHING;

INSERT INTO group_reservations (id, name, organizer_name, organizer_email, capacity, notes)
VALUES
    ('11111111-1111-1111-1111-111111111111', 'Marina Wedding Party',     'Alex Rivera',  'alex@example.com',     40, 'Beachfront ceremony, sunset dinner'),
    ('22222222-2222-2222-2222-222222222222', 'Lighthouse Tech Retreat',  'Priya Shah',   'priya@example.com',    25, 'Engineering offsite, 3 nights'),
    ('33333333-3333-3333-3333-333333333333', 'Harbor Birding Club',      'Sam Okafor',   'sam@example.com',      15, 'Early-morning departures requested')
ON CONFLICT (id) DO NOTHING;
