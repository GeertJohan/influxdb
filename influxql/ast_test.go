package influxql_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/influxdb/influxdb/influxql"
)

// Ensure a value's data type can be retrieved.
func TestInspectDataType(t *testing.T) {
	for i, tt := range []struct {
		v   interface{}
		typ influxql.DataType
	}{
		{float64(100), influxql.Number},
	} {
		if typ := influxql.InspectDataType(tt.v); tt.typ != typ {
			t.Errorf("%d. %v (%s): unexpected type: %s", i, tt.v, tt.typ, typ)
			continue
		}
	}
}

// Ensure the SELECT statement can extract substatements.
func TestSelectStatement_Substatement(t *testing.T) {
	var tests = []struct {
		stmt string
		expr *influxql.VarRef
		sub  string
		err  string
	}{
		// 0. Single series
		{
			stmt: `SELECT value FROM myseries WHERE value > 1`,
			expr: &influxql.VarRef{Val: "value"},
			sub:  `SELECT value FROM myseries WHERE value > 1.000`,
		},

		// 1. Simple join
		{
			stmt: `SELECT sum(aa.value) + sum(bb.value) FROM join(aa,bb)`,
			expr: &influxql.VarRef{Val: "aa.value"},
			sub:  `SELECT aa.value FROM aa`,
		},

		// 2. Simple merge
		{
			stmt: `SELECT sum(aa.value) + sum(bb.value) FROM merge(aa, bb)`,
			expr: &influxql.VarRef{Val: "bb.value"},
			sub:  `SELECT bb.value FROM bb`,
		},

		// 3. Join with condition
		{
			stmt: `SELECT sum(aa.value) + sum(bb.value) FROM join(aa, bb) WHERE aa.host = 'servera' AND bb.host = 'serverb'`,
			expr: &influxql.VarRef{Val: "bb.value"},
			sub:  `SELECT bb.value FROM bb WHERE bb.host = 'serverb'`,
		},

		// 4. Join with complex condition
		{
			stmt: `SELECT sum(aa.value) + sum(bb.value) FROM join(aa, bb) WHERE aa.host = 'servera' AND (bb.host = 'serverb' OR bb.host = 'serverc') AND 1 = 2`,
			expr: &influxql.VarRef{Val: "bb.value"},
			sub:  `SELECT bb.value FROM bb WHERE (bb.host = 'serverb' OR bb.host = 'serverc') AND 1.000 = 2.000`,
		},

		// 5. 4 with different condition order
		{
			stmt: `SELECT sum(aa.value) + sum(bb.value) FROM join(aa, bb) WHERE ((bb.host = 'serverb' OR bb.host = 'serverc') AND aa.host = 'servera') AND 1 = 2`,
			expr: &influxql.VarRef{Val: "bb.value"},
			sub:  `SELECT bb.value FROM bb WHERE ((bb.host = 'serverb' OR bb.host = 'serverc')) AND 1.000 = 2.000`,
		},
	}

	for i, tt := range tests {
		// Parse statement.
		stmt, err := influxql.NewParser(strings.NewReader(tt.stmt)).ParseStatement()
		if err != nil {
			t.Fatalf("invalid statement: %q: %s", tt.stmt, err)
		}

		// Extract substatement.
		sub, err := stmt.(*influxql.SelectStatement).Substatement(tt.expr)
		if err != nil {
			t.Errorf("%d. %q: unexpected error: %s", i, tt.stmt, err)
			continue
		}
		if substr := sub.String(); tt.sub != substr {
			t.Errorf("%d. %q: unexpected substatement:\n\nexp=%s\n\ngot=%s\n\n", i, tt.stmt, tt.sub, substr)
			continue
		}
	}
}

// Ensure the SELECT statement can extract GROUP BY interval.
func TestSelectStatement_GroupByInterval(t *testing.T) {
	q := "SELECT sum(value) from foo GROUP BY time(10m)"
	stmt, err := influxql.NewParser(strings.NewReader(q)).ParseStatement()
	if err != nil {
		t.Fatalf("invalid statement: %q: %s", stmt, err)
	}

	s := stmt.(*influxql.SelectStatement)
	d, err := s.GroupByInterval()
	if d != 10*time.Minute {
		t.Fatalf("group by interval not equal:\nexp=%s\ngot=%s", 10*time.Minute, d)
	}
	if err != nil {
		t.Fatalf("error parsing group by interval: %s", err.Error())
	}
}

// Ensure the SELECT statment can have its start and end time set
func TestSelectStatement_SetTimeRange(t *testing.T) {
	q := "SELECT sum(value) from foo GROUP BY time(10m)"
	stmt, err := influxql.NewParser(strings.NewReader(q)).ParseStatement()
	if err != nil {
		t.Fatalf("invalid statement: %q: %s", stmt, err)
	}

	s := stmt.(*influxql.SelectStatement)
	min, max := influxql.TimeRange(s.Condition)
	start := time.Now().Add(-20 * time.Hour).Round(time.Second).UTC()
	end := time.Now().Add(10 * time.Hour).Round(time.Second).UTC()
	s.SetTimeRange(start, end)
	min, max = influxql.TimeRange(s.Condition)

	if min != start {
		t.Fatalf("start time wasn't set properly.\n  exp: %s\n  got: %s", start, min)
	}
	// the end range is actually one microsecond before the given one since end is exclusive
	end = end.Add(-time.Microsecond)
	if max != end {
		t.Fatalf("end time wasn't set properly.\n  exp: %s\n  got: %s", end, max)
	}

	// ensure we can set a time on a select that already has one set
	start = time.Now().Add(-20 * time.Hour).Round(time.Second).UTC()
	end = time.Now().Add(10 * time.Hour).Round(time.Second).UTC()
	q = fmt.Sprintf("SELECT sum(value) from foo WHERE time >= %ds and time <= %ds GROUP BY time(10m)", start.Unix(), end.Unix())
	stmt, err = influxql.NewParser(strings.NewReader(q)).ParseStatement()
	if err != nil {
		t.Fatalf("invalid statement: %q: %s", stmt, err)
	}

	s = stmt.(*influxql.SelectStatement)
	min, max = influxql.TimeRange(s.Condition)
	if start != min || end != max {
		t.Fatalf("start and end times weren't equal:\n  exp: %s\n  got: %s\n  exp: %s\n  got:%s\n", start, min, end, max)
	}

	// update and ensure it saves it
	start = time.Now().Add(-40 * time.Hour).Round(time.Second).UTC()
	end = time.Now().Add(20 * time.Hour).Round(time.Second).UTC()
	s.SetTimeRange(start, end)
	min, max = influxql.TimeRange(s.Condition)

	// TODO: right now the SetTimeRange can't override the start time if it's more recent than what they're trying to set it to.
	//       shouldn't matter for our purposes with continuous queries, but fix this later

	if min != start {
		t.Fatalf("start time wasn't set properly.\n  exp: %s\n  got: %s", start, min)
	}
	// the end range is actually one microsecond before the given one since end is exclusive
	end = end.Add(-time.Microsecond)
	if max != end {
		t.Fatalf("end time wasn't set properly.\n  exp: %s\n  got: %s", end, max)
	}

	// ensure that when we set a time range other where clause conditions are still there
	q = "SELECT sum(value) from foo WHERE foo = 'bar' GROUP BY time(10m)"
	stmt, err = influxql.NewParser(strings.NewReader(q)).ParseStatement()
	if err != nil {
		t.Fatalf("invalid statement: %q: %s", stmt, err)
	}

	s = stmt.(*influxql.SelectStatement)

	// update and ensure it saves it
	start = time.Now().Add(-40 * time.Hour).Round(time.Second).UTC()
	end = time.Now().Add(20 * time.Hour).Round(time.Second).UTC()
	s.SetTimeRange(start, end)
	min, max = influxql.TimeRange(s.Condition)

	if min != start {
		t.Fatalf("start time wasn't set properly.\n  exp: %s\n  got: %s", start, min)
	}
	// the end range is actually one microsecond before the given one since end is exclusive
	end = end.Add(-time.Microsecond)
	if max != end {
		t.Fatalf("end time wasn't set properly.\n  exp: %s\n  got: %s", end, max)
	}

	// ensure the where clause is there
	hasWhere := false
	influxql.WalkFunc(s.Condition, func(n influxql.Node) {
		if ex, ok := n.(*influxql.BinaryExpr); ok {
			if lhs, ok := ex.LHS.(*influxql.VarRef); ok {
				if lhs.Val == "foo" {
					if rhs, ok := ex.RHS.(*influxql.StringLiteral); ok {
						if rhs.Val == "bar" {
							hasWhere = true
						}
					}
				}
			}
		}
	})
	if !hasWhere {
		t.Fatal("set time range cleared out the where clause")
	}
}

// Ensure an expression can be folded.
func TestFold(t *testing.T) {
	for i, tt := range []struct {
		in  string
		out string
	}{
		// Number literals.
		{`1 + 2`, `3.000`},
		{`(foo*2) + ( (4/2) + (3 * 5) - 0.5 )`, `(foo * 2.000) + 16.500`},
		{`foo(bar(2 + 3), 4)`, `foo(bar(5.000), 4.000)`},
		{`4 / 0`, `0.000`},
		{`4 = 4`, `true`},
		{`4 <> 4`, `false`},
		{`6 > 4`, `true`},
		{`4 >= 4`, `true`},
		{`4 < 6`, `true`},
		{`4 <= 4`, `true`},
		{`4 AND 5`, `4.000 AND 5.000`},

		// Boolean literals.
		{`true AND false`, `false`},
		{`true OR false`, `true`},
		{`true = false`, `false`},
		{`true <> false`, `true`},
		{`true + false`, `true + false`},

		// Time literals.
		{`now() + 2h`, `"2000-01-01 02:00:00"`},
		{`now() / 2h`, `"2000-01-01 00:00:00" / 2h`},
		{`4µ + now()`, `"2000-01-01 00:00:00.000004"`},
		{`now() = now()`, `true`},
		{`now() <> now()`, `false`},
		{`now() < now() + 1h`, `true`},
		{`now() <= now() + 1h`, `true`},
		{`now() >= now() - 1h`, `true`},
		{`now() > now() - 1h`, `true`},
		{`now() - (now() - 60s)`, `1m`},
		{`now() AND now()`, `"2000-01-01 00:00:00" AND "2000-01-01 00:00:00"`},

		// Duration literals.
		{`10m + 1h - 60s`, `69m`},
		{`(10m / 2) * 5`, `25m`},
		{`60s = 1m`, `true`},
		{`60s <> 1m`, `false`},
		{`60s < 1h`, `true`},
		{`60s <= 1h`, `true`},
		{`60s > 12s`, `true`},
		{`60s >= 1m`, `true`},
		{`60s AND 1m`, `1m AND 1m`},
		{`60m / 0`, `0s`},
		{`60m + 50`, `1h + 50.000`},

		// String literals.
		{`'foo' + 'bar'`, `'foobar'`},
	} {
		// Fold expression.
		now := mustParseTime("2000-01-01T00:00:00Z")
		expr := influxql.Fold(MustParseExpr(tt.in), &now)

		// Compare with expected output.
		if out := expr.String(); tt.out != out {
			t.Errorf("%d. %s: unexpected expr:\n\nexp=%s\n\ngot=%s\n\n", i, tt.in, tt.out, out)
			continue
		}
	}
}

// Ensure an a "now()" call is not folded when now is not passed in.
func TestFold_WithoutNow(t *testing.T) {
	expr := influxql.Fold(MustParseExpr(`now()`), nil)
	if s := expr.String(); s != `now()` {
		t.Fatalf("unexpected expr: %s", s)
	}
}

// Ensure the time range of an expression can be extracted.
func TestTimeRange(t *testing.T) {
	for i, tt := range []struct {
		expr     string
		min, max string
	}{
		// LHS VarRef
		{expr: `time > '2000-01-01 00:00:00'`, min: `2000-01-01 00:00:00.000001`, max: `0001-01-01 00:00:00`},
		{expr: `time >= '2000-01-01 00:00:00'`, min: `2000-01-01 00:00:00`, max: `0001-01-01 00:00:00`},
		{expr: `time < '2000-01-01 00:00:00'`, min: `0001-01-01 00:00:00`, max: `1999-12-31 23:59:59.999999`},
		{expr: `time <= '2000-01-01 00:00:00'`, min: `0001-01-01 00:00:00`, max: `2000-01-01 00:00:00`},

		// RHS VarRef
		{expr: `'2000-01-01 00:00:00' > time`, min: `0001-01-01 00:00:00`, max: `1999-12-31 23:59:59.999999`},
		{expr: `'2000-01-01 00:00:00' >= time`, min: `0001-01-01 00:00:00`, max: `2000-01-01 00:00:00`},
		{expr: `'2000-01-01 00:00:00' < time`, min: `2000-01-01 00:00:00.000001`, max: `0001-01-01 00:00:00`},
		{expr: `'2000-01-01 00:00:00' <= time`, min: `2000-01-01 00:00:00`, max: `0001-01-01 00:00:00`},

		// Equality
		{expr: `time = '2000-01-01 00:00:00'`, min: `2000-01-01 00:00:00`, max: `2000-01-01 00:00:00`},

		// Multiple time expressions.
		{expr: `time >= '2000-01-01 00:00:00' AND time < '2000-01-02 00:00:00'`, min: `2000-01-01 00:00:00`, max: `2000-01-01 23:59:59.999999`},

		// Min/max crossover
		{expr: `time >= '2000-01-01 00:00:00' AND time <= '1999-01-01 00:00:00'`, min: `2000-01-01 00:00:00`, max: `1999-01-01 00:00:00`},

		// Absolute time
		{expr: `time = 1388534400s`, min: `2014-01-01 00:00:00`, max: `2014-01-01 00:00:00`},

		// Non-comparative expressions.
		{expr: `time`, min: `0001-01-01 00:00:00`, max: `0001-01-01 00:00:00`},
		{expr: `time + 2`, min: `0001-01-01 00:00:00`, max: `0001-01-01 00:00:00`},
		{expr: `time - '2000-01-01 00:00:00'`, min: `0001-01-01 00:00:00`, max: `0001-01-01 00:00:00`},
		{expr: `time AND '2000-01-01 00:00:00'`, min: `0001-01-01 00:00:00`, max: `0001-01-01 00:00:00`},
	} {
		// Extract time range.
		expr := MustParseExpr(tt.expr)
		min, max := influxql.TimeRange(expr)

		// Compare with expected min/max.
		if min := min.Format(influxql.DateTimeFormat); tt.min != min {
			t.Errorf("%d. %s: unexpected min:\n\nexp=%s\n\ngot=%s\n\n", i, tt.expr, tt.min, min)
			continue
		}
		if max := max.Format(influxql.DateTimeFormat); tt.max != max {
			t.Errorf("%d. %s: unexpected max:\n\nexp=%s\n\ngot=%s\n\n", i, tt.expr, tt.max, max)
			continue
		}
	}
}

// Ensure an AST node can be rewritten.
func TestRewrite(t *testing.T) {
	expr := MustParseExpr(`time > 1 OR foo = 2`)

	// Flip LHS & RHS in all binary expressions.
	act := influxql.RewriteFunc(expr, func(n influxql.Node) influxql.Node {
		switch n := n.(type) {
		case *influxql.BinaryExpr:
			return &influxql.BinaryExpr{Op: n.Op, LHS: n.RHS, RHS: n.LHS}
		default:
			return n
		}
	})

	// Verify that everything is flipped.
	if act := act.String(); act != `2.000 = foo OR 1.000 > time` {
		t.Fatalf("unexpected result: %s", act)
	}
}
