//go:build linux

package net

type (
	ConnectionList struct {
		Head, Tail *Connection
	}
)

func (list *ConnectionList) PushHead(node *Connection) *Connection {
	head := list.Head

	node.Next = head

	if head != nil {
		head.Previous = node
	} else {
		list.Tail = node
	}

	list.Head = node

	return node
}

func (node *Connection) Remove(list *ConnectionList) {
	head := list.Head

	if head == nil {
		goto clear
	}

	if head == node {
		list.Head = head.Next

		goto clear
	}

check:
	if head.Next == nil {
		goto clear
	}

	head = head.Next

	if head == node {
		head.Previous.Next = head.Next

		if head == list.Tail {
			list.Tail = head.Previous
		} else {
			head.Next.Previous = head.Previous
		}

		goto clear
	}

	goto check

clear:
	node.Previous = nil
	node.Next = nil
}
