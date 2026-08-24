#language: pt
Funcionalidade: Identificar contas antigas esquecidas
  Como usuário do e-crawpar
  Quero descobrir em quais serviços tenho conta a partir dos cabeçalhos dos meus e-mails
  Para reencontrar cadastros esquecidos sem nunca abrir o corpo das mensagens

  Cenário: E-mail de atualização de política revela conta antiga
    Dado que existe um e-mail com assunto "Termos de uso atualizados" de "no-reply@mail.oldblog.net" com data de "2021-07-15"
      E que existe um e-mail com assunto "Verify your email" de "noreply@musicsvc.com" com data de "2023-01-10"
    Quando o pipeline processa a caixa de entrada
    Então o relatório final deve listar o domínio "oldblog.net"
      E a categoria do domínio "oldblog.net" deve ser "policy"

  Cenário: Assunto ambíguo é classificado pela prioridade de segurança
    Dado que existe um e-mail com assunto "New device detected - confirm your account" de "security@app.exemplo" com data de "2024-05-01"
    Quando o pipeline processa a caixa de entrada
    Então o relatório final deve listar o domínio "app.exemplo"
      E a categoria do domínio "app.exemplo" deve ser "security"

  Cenário: Múltiplos remetentes do mesmo domínio geram alerta
    Dado que existe um e-mail com assunto "Verify your email" de "remetente-a@duplo.io" com data de "2022-02-02"
      E que existe um e-mail com assunto "Pagamento aprovado" de "remetente-b@duplo.io" com data de "2023-03-03"
    Quando o pipeline processa a caixa de entrada
    Então o relatório final deve listar o domínio "duplo.io"
      E o domínio "duplo.io" deve ter alerta de múltiplos remetentes

  Cenário: Ruído conhecido nunca aparece no relatório
    Dado que existe um e-mail com assunto "Welcome to Netflix" de "info@netflix.com" com data de "2020-06-06"
      E que existe um e-mail com assunto "Promoção semanal da loja" de "news@lojinha.com.br" com data de "2021-08-08"
    Quando o pipeline processa a caixa de entrada
    Então o relatório não deve listar o domínio "netflix.com"
      E o relatório não deve listar o domínio "lojinha.com.br"

  Cenário: Relatório ordenado pela data de nascimento da conta
    Dado que existe um e-mail com assunto "Verify your email" de "novo@recente.org" com data de "2023-12-01"
      E que existe um e-mail com assunto "Welcome to old svc" de "team@antigo.net" com data de "2015-04-04"
    Quando o pipeline processa a caixa de entrada
    Então o primeiro domínio listado deve ser "antigo.net"
      E o último domínio listado deve ser "recente.org"

  Cenário: Prefixo transacional é normalizado para o domínio base
    Dado que existe um e-mail com assunto "Verify your email" de "no-reply@mail.reserva.exemplo" com data de "2019-09-09"
    Quando o pipeline processa a caixa de entrada
    Então o relatório final deve listar o domínio "reserva.exemplo"

  Cenário: E-mail sem categoria não evidencia cadastro
    Dado que existe um e-mail com assunto "Almoço amanhã?" de "colega@pessoal.org" com data de "2024-04-04"
    Quando o pipeline processa a caixa de entrada
    Então o relatório não deve listar o domínio "pessoal.org"
