# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.13'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.13/aviary_v1.0.13_darwin_arm64.tar.gz'
      sha256 '0fdef557a873b509763f48f99c86be3b37e7f519e6f10a230e15278f2708fb31'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.13/aviary_v1.0.13_darwin_amd64.tar.gz'
      sha256 '4f23f5e38a0590139194982ac1574d6ae06d385d0f6c8e1fd847878b3b5a3a18'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.13/aviary_v1.0.13_linux_arm64.tar.gz'
      sha256 '23ce8bdd153de9afca640073838d8c4467e64393b68aa15e3c653f47ea08b1e9'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.13/aviary_v1.0.13_linux_amd64.tar.gz'
      sha256 'cd29fdc74c0050fcfbb1f0fdd3c5cce5fb5d9811926c423a002bdb46dad7b41c'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
