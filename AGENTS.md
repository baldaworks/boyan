# Prism

Каждая фаза Prism выполняется в отдельном субагенте. Основной агент
координирует переходы между фазами и передаёт результаты между субагентами.

Story закрывается только после слияния всех её изменений в целевую ветку
и успешной проверки слитого состояния. Локальные проверки до слияния
не являются основанием для закрытия story.

# Testing

Integration tests must run separately and only with the `integration` build tag.
The default `go test ./...` must not run integration tests.
CI may run integration tests for pull requests targeting `main` and for releases.
