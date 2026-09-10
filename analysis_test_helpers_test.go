package actionlint

import "errors"

const commandGoodWorkflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
const commandBadWorkflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ missing.value }}'\n"

type commandFailingIO struct{}

func (commandFailingIO) Read([]byte) (int, error)  { return 0, errors.New("read failed") }
func (commandFailingIO) Write([]byte) (int, error) { return 0, errors.New("write failed") }
