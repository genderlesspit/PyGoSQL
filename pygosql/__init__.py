from .pygosql import PyGoSQL

def get_default_go_file():
    from pathlib import Path
    return Path(__file__).parent / "gosql" / "main.go"

go_file = get_default_go_file()