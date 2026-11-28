# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.9'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.9/aviary_v1.0.9_darwin_arm64.tar.gz'
      sha256 '8395d33c3702098e4b1a0d36a411d2dfc836995d81b724b81c37c595c2226a2c'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.9/aviary_v1.0.9_darwin_amd64.tar.gz'
      sha256 '3a2ac5e7a03ae769cbd14d85e381db181799f147fd8406d21f899737eac8821e'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.9/aviary_v1.0.9_linux_arm64.tar.gz'
      sha256 '2425b3176c32095fa2721cd06476a116982b16e38e8104442a0aeab327a1df76'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.9/aviary_v1.0.9_linux_amd64.tar.gz'
      sha256 'ed94d45cd37f05b00c04491868ee4ff9186da19b92d10ff22c5b18dc5f962b1b'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
