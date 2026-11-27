# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.4'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.4/aviary_v1.0.4_darwin_arm64.tar.gz'
      sha256 '0143351637860e117b72d752a80401afe358857116175450f05cf077195cac4c'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.4/aviary_v1.0.4_darwin_amd64.tar.gz'
      sha256 '9a0b17f766d59fd84593c1935e5c45b2a8d6de735e6504776c6c27b5cfcc0f5b'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.4/aviary_v1.0.4_linux_arm64.tar.gz'
      sha256 '9e4b51e60caf90387d80724e43d0b257c776fd6d8ba600212a048238607328d7'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.4/aviary_v1.0.4_linux_amd64.tar.gz'
      sha256 '072151a86489eb6027f92a3e23fda6b004459cc5c6457c3d2700e29428151212'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
