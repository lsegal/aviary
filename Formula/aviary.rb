# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.11'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.11/aviary_v1.0.11_darwin_arm64.tar.gz'
      sha256 '89a8152cdfc519be37cb8659e9d7639d85da5672dafa32bc9de50f5a6dd36c07'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.11/aviary_v1.0.11_darwin_amd64.tar.gz'
      sha256 '05a7d32ec4a11837c125dfa7ebe0f252245ba780b0acdcda2948419c881667df'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.11/aviary_v1.0.11_linux_arm64.tar.gz'
      sha256 '7917a6e8eda331ffa88bb74371256ee75b7cc51f439baba8418b80a081e38615'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.11/aviary_v1.0.11_linux_amd64.tar.gz'
      sha256 '78f96b2ecf79736b30aebbb52b6bb88063641fdf885ac4659319a2358af5fd46'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
