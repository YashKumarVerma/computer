package sdk

type state struct {
	Operations map[Namespace]map[string]*Operation
}

type Namespace struct {
	Name string
}

func (c *Client) RegisterNamespace(name string) Namespace {
	ns := Namespace{
		Name: name,
	}
	c.state.Operations[ns] = make(map[string]*Operation)
	return ns
}
