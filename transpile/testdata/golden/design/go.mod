// design is a component-library fixture for the one-state golden cases: a
// component of another package taking a State-typed prop, which the
// compiler can't follow — escape means synced. The golden module replaces
// it to this directory.
module vukatest.local/design

go 1.25.0

require vukatest.local/stateui v0.0.0

replace vukatest.local/stateui => ../stateui
