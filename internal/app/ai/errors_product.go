package ai

import "fmt"

// ProductNotFoundError: a drafted line names a product the books do not have.
// The confirm handler turns it into a prompt that names the product.
type ProductNotFoundError struct {
	Name string
}

func (e *ProductNotFoundError) Error() string {
	return fmt.Sprintf("plan executor: product %q not found — cannot create draft", e.Name)
}
