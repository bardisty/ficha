// Package pricing is the model catalog: per-model token rates, display
// names and context windows, and the rules for resolving a dated or
// decorated model ID to its catalog row. Adding a model is one row in
// modelCatalog; CONTRIBUTING.md has the steps and the tests it needs.
//
// A model the catalog doesn't know gets fallback rates, and IsKnownModel
// tells callers when that happened so they can warn. The package imports no
// other ficha package.
package pricing
