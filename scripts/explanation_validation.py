"""Reject bibliographic footers in contributed explanations."""
from html import unescape
from html.parser import HTMLParser
import re


PAGE_REFERENCE = re.compile(r'\b(?:p[aá]gina|pages?|p[aá]g\.|pp?\.|страниц[аы]|стр\.)\s*\d+\b', re.I)
BOOK_REFERENCE = re.compile(r'\b(?:manual|textbook|handbook|book|libro|учебник)\s*(?:[\"\x27“‘«]|titled\b)', re.I)


class ExplanationText(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.parts = []

    def handle_data(self, data):
        self.parts.append(data)


def validate_explanations(question):
    fields = list(question['help'].values())
    markup = question.get('help_html', '')
    parser = ExplanationText()
    parser.feed(markup)
    fields.extend((markup, ''.join(parser.parts)))
    for value in fields:
        value = unescape(value)
        if PAGE_REFERENCE.search(value) or BOOK_REFERENCE.search(value):
            raise ValueError(f'Bibliographic reference in explanation for {question["id"]}')
