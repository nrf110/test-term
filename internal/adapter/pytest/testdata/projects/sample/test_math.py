import pytest


def add(a, b):
    return a + b


def test_add_positive():
    assert add(1, 2) == 3


def test_add_fails():
    assert add(1, 1) == 3


@pytest.mark.skip(reason="not ready")
def test_skipped():
    assert False


@pytest.mark.parametrize("a,b,expected", [(1, 1, 2), (2, 3, 5)])
def test_param(a, b, expected):
    assert add(a, b) == expected


class TestGroup:
    def test_method(self):
        assert add(2, 2) == 4
