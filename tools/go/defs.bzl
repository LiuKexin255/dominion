load("@rules_go//go:def.bzl", "go_test")


def _append_unique(values, item):
    result = list(values)
    if item not in result:
        result.append(item)
    return result


def _with_verbose_args(kwargs):
    updated = dict(kwargs)
    updated["args"] = _append_unique(updated.get("args", []), "-test.v")
    return updated


def go_unittest(name, **kwargs):
    updated = _with_verbose_args(kwargs)
    updated.setdefault("size", "small")
    go_test(
        name = name,
        **updated
    )


def go_largetest(name, **kwargs):
    updated = _with_verbose_args(kwargs)
    updated["tags"] = _append_unique(updated.get("tags", []), "manual")
    go_test(
        name = name,
        **updated
    )
