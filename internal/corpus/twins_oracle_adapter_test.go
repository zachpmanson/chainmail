package corpus

// The portable harness is overlaid alone on the pinned source. This local
// adapter supplies a second, uncached application path for clone parity.
func init() { twinsOracleBaseline = uncachedOracleRound }

func uncachedOracleRound(s *Store) error {
	plan, err := CollapseTwins(s, false)
	if err != nil {
		return err
	}
	for _, c := range plan.Collapse {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if err = collapseGroup(tx, s, c); err == nil {
			err = tx.Commit()
		}
		tx.Rollback()
		if err != nil {
			return err
		}
	}
	for _, d := range plan.Derived {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if err = persistDerived(tx, d.Copy, d.Base); err == nil {
			err = tx.Commit()
		}
		tx.Rollback()
		if err != nil {
			return err
		}
	}
	if _, err := RepairQuotedBodies(s); err != nil {
		return err
	}
	if _, err := RepairMailtoIdentities(s); err != nil {
		return err
	}
	if _, err := RepairPlusAddresses(s); err != nil {
		return err
	}
	if _, err := RepairTruncatedNames(s); err != nil {
		return err
	}
	if _, err := s.RepairGraph(); err != nil {
		return err
	}
	if _, err := s.ReassertParents(); err != nil {
		return err
	}
	_, err = s.RepairDanglingQuoteParents()
	return err
}
