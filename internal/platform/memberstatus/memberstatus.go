// Package memberstatus holds the one SQL definition of a member's coverage, shared by the modules that
// show a member's status (members list and detail, reports dashboard) so they can never disagree.
//
// A member may hold several periods at once, one per plan. The rule (business-rules.md, section 4):
//
//   - a period is CURRENT when starts_on <= today <= ends_on (both ends inclusive);
//   - the member is covered when any period, of any plan, is current;
//   - the COVERAGE END is where that coverage stops: start from the latest end among the current
//     periods and keep extending it with any period that begins on or before the day after it
//     (a renewal queued behind the current period, or another plan that continues it);
//   - days remaining = coverage end - today; "expiring" when that is 7 days or fewer.
package memberstatus

// Prefix returns the beginning of a query: `WITH RECURSIVE cov(...), coverage AS (...)`. The caller
// continues with `, other AS (...) SELECT ...` and joins `coverage` (member_id, end_on), which has one row
// per currently covered member and no row for the rest. gym and today are SQL expressions such as
// "$1" and "$2::date"; the coverage is limited to the gym.
func Prefix(gym, today string) string {
	return `WITH RECURSIVE cov(member_id, end_on) AS (
  SELECT member_id, max(ends_on) FROM memberships
   WHERE gym_id = ` + gym + ` AND starts_on <= ` + today + ` AND ends_on >= ` + today + ` GROUP BY member_id
  UNION
  SELECT cov.member_id, ms.ends_on FROM cov
   JOIN memberships ms ON ms.member_id = cov.member_id AND ms.gym_id = ` + gym + `
    AND ms.starts_on <= cov.end_on + 1 AND ms.ends_on > cov.end_on
), coverage AS (SELECT member_id, max(end_on) AS end_on FROM cov GROUP BY member_id)`
}
