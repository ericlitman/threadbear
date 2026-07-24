package codex

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

var requiredThreadColumns = []string{
	"id",
	"updated_at",
	"title",
	"archived",
	"source",
	"thread_source",
	"rollout_path",
}

func (i *Index) validateSchema(ctx context.Context) error {
	rows, err := i.db.QueryContext(ctx, "PRAGMA table_info(threads)")
	if err != nil {
		return fmt.Errorf("%w: inspect threads: %v", ErrSchema, err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var sequence int
		var name string
		var dataType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&sequence, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("%w: read threads columns: %v", ErrSchema, err)
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: inspect threads: %v", ErrSchema, err)
	}
	missing := make([]string, 0)
	for _, name := range requiredThreadColumns {
		if _, ok := columns[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%w: threads missing %s", ErrSchema, strings.Join(missing, ","))
	}
	return nil
}
