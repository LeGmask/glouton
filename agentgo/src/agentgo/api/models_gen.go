// Code generated by github.com/99designs/gqlgen, DO NOT EDIT.

package api

type Label struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type LabelInput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Labels struct {
	Labels []*LabelInput `json:"labels"`
}

type Metric struct {
	Labels []*Label `json:"labels"`
}
